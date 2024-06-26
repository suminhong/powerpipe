package db_client

import (
	"context"
	"github.com/turbot/pipe-fittings/utils"
	"github.com/turbot/powerpipe/internal/queryresult"
)

// ExecuteQuery executes a single query. If shutdownAfterCompletion is true, shutdown the client after completion
func ExecuteQuery(ctx context.Context, client DbClient, queryString string, args ...any) (*queryresult.ResultStreamer, error) {
	utils.LogTime("db.ExecuteQuery start")
	defer utils.LogTime("db.ExecuteQuery end")

	resultsStreamer := queryresult.NewResultStreamer()
	result, err := client.Execute(ctx, queryString, args...)
	if err != nil {
		return nil, err
	}
	go func() {
		resultsStreamer.StreamResult(result)
		resultsStreamer.Close()
	}()
	return resultsStreamer, nil
}
