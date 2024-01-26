package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/turbot/go-kit/helpers"
	"github.com/turbot/pipe-fittings/cmdconfig"
	"github.com/turbot/pipe-fittings/constants"
	"github.com/turbot/pipe-fittings/contexthelpers"
	"github.com/turbot/pipe-fittings/error_helpers"
	"github.com/turbot/pipe-fittings/modconfig"
	"github.com/turbot/pipe-fittings/statushooks"
	"github.com/turbot/pipe-fittings/utils"
	localcmdconfig "github.com/turbot/powerpipe/internal/cmdconfig"
	localconstants "github.com/turbot/powerpipe/internal/constants"
	"github.com/turbot/powerpipe/internal/controldisplay"
	"github.com/turbot/powerpipe/internal/controlexecute"
	"github.com/turbot/powerpipe/internal/controlinit"
	"github.com/turbot/powerpipe/internal/controlstatus"
	"github.com/turbot/steampipe-plugin-sdk/v5/sperr"
)

// generic command to handle benchmark and control execution
func checkCmd[T controlinit.CheckTarget]() *cobra.Command {
	typeName := localcmdconfig.GetGenericTypeName[T]()
	cmd := &cobra.Command{
		Use:              checkCmdUse(typeName),
		TraverseChildren: true,
		Args:             cobra.ExactArgs(1),
		Run:              runCheckCmd[T],
		Short:            checkCmdShort(typeName),
		Long:             checkCmdLong(typeName),
	}

	builder := cmdconfig.OnCmd(cmd)
	builder.
		AddCloudFlags().
		AddWorkspaceDatabaseFlag().
		AddModLocationFlag().
		AddBoolFlag(constants.ArgHeader, true, "Include column headers for csv and table output").
		AddBoolFlag(constants.ArgHelp, false, "Help for run command", cmdconfig.FlagOptions.WithShortHand("h")).
		AddStringFlag(constants.ArgSeparator, ",", "Separator string for csv output").
		AddStringFlag(constants.ArgOutput, constants.OutputFormatText, "Output format: brief, csv, html, json, md, text, snapshot or none").
		AddBoolFlag(constants.ArgTiming, false, "Turn on the timer which reports run time").
		AddStringSliceFlag(constants.ArgSearchPath, nil, "Set a custom search_path (comma-separated)").
		AddStringSliceFlag(constants.ArgSearchPathPrefix, nil, "Set a prefix to the current search path (comma-separated)").
		AddStringSliceFlag(constants.ArgExport, nil, "Export output to file, supported formats: csv, html, json, md, nunit3, sps (snapshot), asff").
		AddBoolFlag(constants.ArgProgress, true, "Display control execution progress").
		AddStringSliceFlag(constants.ArgVarFile, nil, "Specify an .ppvar file containing variable values").
		// NOTE: use StringArrayFlag for ArgVariable, not StringSliceFlag
		// Cobra will interpret values passed to a StringSliceFlag as CSV,
		// where args passed to StringArrayFlag are not parsed and used raw
		AddStringArrayFlag(constants.ArgVariable, nil, "Specify the value of a variable").
		AddIntFlag(constants.ArgDatabaseQueryTimeout, localconstants.DatabaseDefaultCheckQueryTimeout, "The query timeout").
		AddBoolFlag(constants.ArgModInstall, true, "Specify whether to install mod dependencies before running").
		AddBoolFlag(constants.ArgInput, true, "Enable interactive prompts").
		AddBoolFlag(constants.ArgSnapshot, false, "Create snapshot in Turbot Pipes with the default (workspace) visibility").
		AddBoolFlag(constants.ArgShare, false, "Create snapshot in Turbot Pipes with 'anyone_with_link' visibility").
		AddStringArrayFlag(constants.ArgSnapshotTag, nil, "Specify tags to set on the snapshot").
		AddStringFlag(constants.ArgSnapshotLocation, "", "The location to write snapshots - either a local file path or a Turbot Pipes workspace").
		AddStringFlag(constants.ArgSnapshotTitle, "", "The title to give a snapshot")

	// for control command, add --arg
	switch typeName {
	case "control":
		builder.AddStringArrayFlag(constants.ArgArg, nil, "Specify the value of a control argument")
	case "benchmark":
		builder.
			AddStringFlag(constants.ArgWhere, "", "SQL 'where' clause, or named query, used to filter controls (cannot be used with '--tag')").
			AddBoolFlag(constants.ArgDryRun, false, "Show which controls will be run without running them").
			AddStringSliceFlag(constants.ArgTag, nil, "Filter controls based on their tag values ('--tag key=value')").
			AddIntFlag(constants.ArgMaxParallel, constants.DefaultMaxConnections, "The maximum number of concurrent database connections to open")
	}

	return cmd
}

func checkCmdUse(typeName string) string {
	return fmt.Sprintf("run [flags] [%s]", typeName)
}
func checkCmdShort(typeName string) string {
	return fmt.Sprintf("Execute one or more %ss", typeName)
}
func checkCmdLong(typeName string) string {
	return fmt.Sprintf(`Execute one or more %ss.

You may specify one or more benchmarks to run, separated by a space.`, typeName)
}

// exitCode=0 no runtime errors, no control alarms or errors
// exitCode=1 no runtime errors, 1 or more control alarms, no control errors
// exitCode=2 no runtime errors, 1 or more control errors
// exitCode=3+ runtime errors

func runCheckCmd[T controlinit.CheckTarget](cmd *cobra.Command, args []string) {
	utils.LogTime("runCheckCmd start")

	// setup a cancel context and start cancel handler
	ctx, cancel := context.WithCancel(cmd.Context())
	contexthelpers.StartCancelHandler(cancel)

	defer func() {
		utils.LogTime("runCheckCmd end")
		if r := recover(); r != nil {
			error_helpers.ShowError(ctx, helpers.ToError(r))
			exitCode = constants.ExitCodeUnknownErrorPanic
		}
	}()

	// validate the arguments
	err := validateCheckArgs(ctx)
	if err != nil {
		exitCode = constants.ExitCodeInsufficientOrWrongInputs
		error_helpers.ShowError(ctx, err)
		return
	}
	// if diagnostic mode is set, print out config and return
	if _, ok := os.LookupEnv(localconstants.EnvConfigDump); ok {
		localcmdconfig.DisplayConfig()
		return
	}

	// show the status spinner
	statushooks.Show(ctx)

	// initialise
	statushooks.SetStatus(ctx, "Initializing...")
	// disable status hooks in init - otherwise we will end up
	// getting status updates all the way down from the service layer
	initCtx := statushooks.DisableStatusHooks(ctx)
	initData := controlinit.NewInitData[T](initCtx, args)
	if initData.Result.Error != nil {
		exitCode = constants.ExitCodeInitializationFailed
		error_helpers.ShowError(ctx, initData.Result.Error)
		return
	}
	defer initData.Cleanup(ctx)

	// hide the spinner so that warning messages can be shown
	statushooks.Done(ctx)

	// if there is a usage warning we display it
	initData.Result.DisplayMessages()

	// get the execution trees
	namedTree, err := getExecutionTree(ctx, initData)
	error_helpers.FailOnError(err)

	// execute controls synchronously (execute returns the number of alarms and errors)

	// pull out useful properties
	totalAlarms, totalErrors := 0, 0
	defer func() {
		// set the defined exit code after successful execution
		exitCode = getExitCode(totalAlarms, totalErrors)
	}()

	err = executeTree(ctx, namedTree.tree, initData)
	if err != nil {
		totalErrors++
		error_helpers.ShowError(ctx, err)
		return
	}

	// append the total number of alarms and errors for multiple runs
	totalAlarms = namedTree.tree.Root.Summary.Status.Alarm
	totalErrors = namedTree.tree.Root.Summary.Status.Error

	err = publishSnapshot(ctx, namedTree.tree, viper.GetBool(constants.ArgShare), viper.GetBool(constants.ArgSnapshot))
	if err != nil {
		error_helpers.ShowError(ctx, err)
		totalErrors++
		return
	}

	printTiming(namedTree.tree)

	err = exportExecutionTree(ctx, namedTree, initData, viper.GetStringSlice(constants.ArgExport))
	if err != nil {
		error_helpers.ShowError(ctx, err)
		totalErrors++
	}
}

// exportExecutionTree relies on the fact that the given tree is already executed
func exportExecutionTree(ctx context.Context, namedTree *namedExecutionTree, initData *controlinit.InitData, exportArgs []string) error {
	statushooks.Show(ctx)
	defer statushooks.Done(ctx)

	if error_helpers.IsContextCanceled(ctx) {
		return ctx.Err()
	}

	exportMsg, err := initData.ExportManager.DoExport(ctx, namedTree.name, namedTree.tree, exportArgs)
	if err != nil {
		return err
	}

	// print the location where the file is exported if progress=true
	if len(exportMsg) > 0 && viper.GetBool(constants.ArgProgress) {
		fmt.Printf("\n%s\n", strings.Join(exportMsg, "\n")) //nolint:forbidigo // we want to print
	}

	return nil
}

// executeTree executes and displays the (table) results of an execution
func executeTree(ctx context.Context, tree *controlexecute.ExecutionTree, initData *controlinit.InitData) error {
	// create a context with check status hooks
	checkCtx := createCheckContext(ctx)
	err := tree.Execute(checkCtx)
	if err != nil {
		return err
	}

	err = displayControlResults(checkCtx, tree, initData.OutputFormatter)
	if err != nil {
		return err
	}
	return nil
}

func publishSnapshot(ctx context.Context, executionTree *controlexecute.ExecutionTree, shouldShare bool, shouldUpload bool) error {
	if error_helpers.IsContextCanceled(ctx) {
		return ctx.Err()
	}
	// if the share args are set, create a snapshot and share it
	if shouldShare || shouldUpload {
		statushooks.SetStatus(ctx, "Publishing snapshot")
		return controldisplay.PublishSnapshot(ctx, executionTree, shouldShare)
	}
	return nil
}

func getExecutionTree(ctx context.Context, initData *controlinit.InitData) (*namedExecutionTree, error) {
	// todo kai needed???
	if error_helpers.IsContextCanceled(ctx) {
		return nil, ctx.Err()
	}

	target := initData.Target
	executionTree, err := controlexecute.NewExecutionTree(ctx, initData.Workspace, initData.Client, initData.ControlFilterWhereClause, target)
	if err != nil {
		return nil, sperr.WrapWithMessage(err, "could not create merged execution tree")
	}

	var name string
	if initData.ExportManager.HasNamedExport(viper.GetStringSlice(constants.ArgExport)) {
		name = fmt.Sprintf("check.%s", initData.Workspace.Mod.ShortName)
	} else {
		name = getExportName(target.Name(), initData.Workspace.Mod.ShortName)
	}

	return newNamedExecutionTree(name, executionTree), ctx.Err()

}

// getExportName resolves the base name of the target file
func getExportName(targetName string, modShortName string) string {
	parsedName, _ := modconfig.ParseResourceName(targetName)
	if targetName == "all" {
		// there will be no block type = manually construct name
		return fmt.Sprintf("%s.%s", modShortName, parsedName.Name)
	}
	// default to just converting to valid resource name
	return parsedName.ToFullNameWithMod(modShortName)
}

// get the exit code for successful check run
func getExitCode(alarms int, errors int) int {
	// 1 or more control errors, return exitCode=2
	if errors > 0 {
		return constants.ExitCodeControlsError
	}
	// 1 or more controls in alarm, return exitCode=1
	if alarms > 0 {
		return constants.ExitCodeControlsAlarm
	}
	// no controls in alarm/error
	return constants.ExitCodeSuccessful
}

// create the context for the check run - add a control status renderer
func createCheckContext(ctx context.Context) context.Context {
	return controlstatus.AddControlHooksToContext(ctx, controlstatus.NewStatusControlHooks())
}

func validateCheckArgs(ctx context.Context) error {

	if err := localcmdconfig.ValidateSnapshotArgs(ctx); err != nil {
		return err
	}

	if viper.IsSet(constants.ArgSearchPath) && viper.IsSet(constants.ArgSearchPathPrefix) {
		return fmt.Errorf("only one of --search-path or --search-path-prefix may be set")
	}

	// only 1 character is allowed for '--separator'
	if len(viper.GetString(constants.ArgSeparator)) > 1 {
		return fmt.Errorf("'--%s' can be 1 character long at most", constants.ArgSeparator)
	}

	// only 1 of 'share' and 'snapshot' may be set
	if viper.GetBool(constants.ArgShare) && viper.GetBool(constants.ArgSnapshot) {
		return fmt.Errorf("only 1 of '--%s' and '--%s' may be set", constants.ArgShare, constants.ArgSnapshot)
	}

	// if both '--where' and '--tag' have been used, then it's an error
	if viper.IsSet(constants.ArgWhere) && viper.IsSet(constants.ArgTag) {
		return fmt.Errorf("only 1 of '--%s' and '--%s' may be set", constants.ArgWhere, constants.ArgTag)
	}

	return nil
}

func printTiming(tree *controlexecute.ExecutionTree) {
	// TODO KAI
	//if !shouldPrintTiming() {
	//	return
	//}
	//headers := []string{"", "Duration"}
	//var rows [][]string
	//
	//for _, rg := range tree.Root.Groups {
	//	if rg.GroupItem.GetUnqualifiedName() == "benchmark.root" {
	//		// this is the created root benchmark
	//		// adds the children
	//		for _, g := range rg.Groups {
	//			rows = append(rows, []string{g.GroupItem.GetUnqualifiedName(), rg.Duration.String()})
	//		}
	//		continue
	//	}
	//	rows = append(rows, []string{rg.GroupItem.GetUnqualifiedName(), rg.Duration.String()})
	//}
	//for _, c := range tree.Root.ControlRuns {
	//	rows = append(rows, []string{c.Control.GetUnqualifiedName(), c.Duration.String()})
	//}
	//// blank line after renderer output
	//fmt.Println()
	//fmt.Println("Timing:")
	//display.ShowWrappedTable(headers, rows, &display.ShowWrappedTableOptions{AutoMerge: false})
}

//
//func shouldPrintTiming() bool {
//	outputFormat := viper.GetString(constants.ArgOutput)
//
//	return (viper.GetBool(constants.ArgTiming) && !viper.GetBool(constants.ArgDryRun)) &&
//		(outputFormat == constants.OutputFormatText || outputFormat == constants.OutputFormatBrief)
//}

func displayControlResults(ctx context.Context, executionTree *controlexecute.ExecutionTree, formatter controldisplay.Formatter) error {
	reader, err := formatter.Format(ctx, executionTree)
	if err != nil {
		return err
	}
	_, err = io.Copy(os.Stdout, reader)
	return err
}

type namedExecutionTree struct {
	tree *controlexecute.ExecutionTree
	name string
}

func newNamedExecutionTree(name string, tree *controlexecute.ExecutionTree) *namedExecutionTree {
	return &namedExecutionTree{
		tree: tree,
		name: name,
	}
}
