import Benchmark from "./Benchmark";
import {
  BasePrimitiveProps,
  ExecutablePrimitiveProps,
  LeafNodeData,
} from "../../common";
import { DashboardRunState } from "@powerpipe/types";

export type CheckNodeType =
  | "benchmark"
  | "control"
  | "control_tag"
  | "dimension"
  | "empty_result"
  | "error"
  | "reason"
  | "resource"
  | "result"
  | "running"
  | "root"
  | "severity"
  | "status";

export type CheckNode = {
  sort: string;
  name: string;
  title: string;
  type: CheckNodeType;
  status: CheckNodeStatus;
  severity?: CheckSeverity;
  severity_summary: CheckSeveritySummary;
  summary: CheckSummary;
  children?: CheckNode[];
  data?: LeafNodeData;
  error?: string;
  merge?: (other: CheckNode) => void;
};

export type CheckNodeStatus = "running" | "complete";

export type CheckSeverity = "none" | "low" | "medium" | "high" | "critical";

export type CheckSeveritySummary =
  | {}
  | {
      [key in CheckSeverity]: number;
    };

export type CheckSummary = {
  alarm: number;
  ok: number;
  info: number;
  skip: number;
  error: number;
};

export type CheckDynamicValueMap = {
  [dimension: string]: boolean;
};

export type CheckDynamicColsMap = {
  dimensions: CheckDynamicValueMap;
  tags: CheckDynamicValueMap;
};

export type CheckTags = {
  [key: string]: string;
};

export type CheckResultDimension = {
  key: string;
  value: string;
};

export enum CheckResultStatus {
  alarm = "alarm",
  ok = "ok",
  info = "info",
  skip = "skip",
  error = "error",
  empty = "empty",
}

export type CheckResultType = "loading" | "error" | "empty" | "result";

export type CheckResult = {
  dimensions: CheckResultDimension[];
  tags: CheckTags;
  control: CheckNode;
  benchmark_trunk: Benchmark[];
  status: CheckResultStatus;
  reason: string;
  resource: string;
  severity?: CheckSeverity;
  error?: string;
  type: CheckResultType;
};

type CheckControlRunProperties = {
  severity?: CheckSeverity | undefined;
};

export type CheckControlRun = {
  name: string;
  title?: string;
  description?: string;
  panel_type: "control";
  properties?: CheckControlRunProperties;
  severity?: CheckSeverity | undefined;
  tags?: CheckTags;
  data: LeafNodeData;
  summary: CheckSummary;
  status: DashboardRunState;
  error?: string;
};

export type CheckDisplayGroupType =
  | "benchmark"
  | "control"
  | "control_tag"
  | "result"
  | "dimension"
  | "reason"
  | "resource"
  | "severity"
  | "status"
  | string;

export type CheckDisplayGroup = {
  type: CheckDisplayGroupType;
  value?: string | undefined;
};

export type CheckFilterOperator = "and" | "equal";

export type CheckFilter = {
  operator: CheckFilterOperator;
  type?: CheckFilterType;
  key?: string;
  value?: string;
  title?: string;
  expressions?: CheckFilter[];
};

export type CheckFilterType = CheckDisplayGroupType;

export type BenchmarkTreeProps = BasePrimitiveProps &
  ExecutablePrimitiveProps & {
    properties: {
      grouping: CheckNode;
      first_child_summaries: CheckSummary[];
    };
  };

export type AddControlResultsAction = (results: CheckResult[]) => void;

export const findDimension = (
  dimensions?: CheckResultDimension[],
  key?: string,
) => {
  if (!dimensions || !key) {
    return undefined;
  }
  return dimensions.find((d) => d.key === key);
};
