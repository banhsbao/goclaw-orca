import { describe, expect, it } from "vitest";
import {
  buildOrcaServerCreatePayload,
  buildOrcaServerUpdatePayload,
  validateOrcaServerForm,
  type OrcaServerFormState,
} from "../orca-server-form-helpers";

function form(overrides: Partial<OrcaServerFormState> = {}): OrcaServerFormState {
  return { label: "prod-1", description: "Prod box", pairingCode: "orca://abc", ...overrides };
}

describe("validateOrcaServerForm", () => {
  it("accepts a valid create form", () => {
    expect(validateOrcaServerForm(form(), "create")).toBeNull();
  });

  it.each(["", "Upper", "-lead", "has space", "a".repeat(33)])("rejects invalid label %j", (label) => {
    expect(validateOrcaServerForm(form({ label }), "create")).toBe("labelInvalid");
  });

  it("accepts a 32 character label", () => {
    expect(validateOrcaServerForm(form({ label: "a".repeat(32) }), "create")).toBeNull();
  });

  it("rejects the reserved label default", () => {
    expect(validateOrcaServerForm(form({ label: "default" }), "create")).toBe("labelReserved");
  });

  it("rejects descriptions over 200 characters", () => {
    expect(validateOrcaServerForm(form({ description: "x".repeat(201) }), "create")).toBe("descriptionTooLong");
  });

  it("requires a pairing code on create", () => {
    expect(validateOrcaServerForm(form({ pairingCode: "  " }), "create")).toBe("codeRequired");
  });

  it("rejects a pairing code without the orca:// prefix", () => {
    expect(validateOrcaServerForm(form({ pairingCode: "http://x" }), "create")).toBe("codeInvalid");
  });

  it("allows an empty pairing code on edit and ignores the label", () => {
    expect(validateOrcaServerForm(form({ label: "", pairingCode: "" }), "edit")).toBeNull();
  });

  it("still validates a replacement pairing code on edit", () => {
    expect(validateOrcaServerForm(form({ pairingCode: "nope" }), "edit")).toBe("codeInvalid");
  });
});

describe("orca server payload contract", () => {
  it("builds a snake_case create payload and omits an empty description", () => {
    expect(buildOrcaServerCreatePayload(form({ description: " " }))).toEqual({
      label: "prod-1",
      pairing_code: "orca://abc",
    });
  });

  it("includes the trimmed description on create", () => {
    expect(buildOrcaServerCreatePayload(form({ description: " hi " })).description).toBe("hi");
  });

  it("omits pairing_code on edit when the field is empty", () => {
    expect(buildOrcaServerUpdatePayload(form({ pairingCode: "" }))).toEqual({ description: "Prod box" });
  });

  it("sends the replacement pairing_code on edit when provided", () => {
    expect(buildOrcaServerUpdatePayload(form({ pairingCode: " orca://new " }))).toEqual({
      description: "Prod box",
      pairing_code: "orca://new",
    });
  });
});
