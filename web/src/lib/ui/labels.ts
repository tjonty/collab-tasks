import type { Priority, Status } from "@/lib/api/types";

export const STATUS_LABEL: Record<Status, string> = {
  todo: "To do",
  in_progress: "In progress",
  in_review: "In review",
  done: "Done",
};

export const PRIORITY_LABEL: Record<Priority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
};

export const STATUSES: Status[] = ["todo", "in_progress", "in_review", "done"];
export const PRIORITIES: Priority[] = ["urgent", "high", "medium", "low"];
