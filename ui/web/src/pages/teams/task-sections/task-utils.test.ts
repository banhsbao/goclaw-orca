import { describe, expect, it } from "vitest";
import { isAwaitingLeaderApproval } from "./task-utils";

describe("isAwaitingLeaderApproval", () => {
  it("is true for a pending task held for leader approval", () => {
    expect(isAwaitingLeaderApproval({ status: "pending", metadata: { awaiting_leader_approval: true } })).toBe(true);
  });

  it("is false once the request left pending", () => {
    expect(isAwaitingLeaderApproval({ status: "in_progress", metadata: { awaiting_leader_approval: true } })).toBe(false);
  });

  it("is false for pending tasks without the hold flag", () => {
    expect(isAwaitingLeaderApproval({ status: "pending" })).toBe(false);
    expect(isAwaitingLeaderApproval({ status: "pending", metadata: { awaiting_leader_approval: "true" } })).toBe(false);
  });
});
