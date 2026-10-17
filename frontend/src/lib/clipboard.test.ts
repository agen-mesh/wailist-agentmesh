import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const platform = vi.hoisted(() => ({ native: false }));
vi.mock("./nativeAuth", () => ({
  get IS_NATIVE() {
    return platform.native;
  },
}));

import { copyText } from "./clipboard";

const writeText = vi.fn();
const execCommand = vi.fn();

beforeEach(() => {
  platform.native = false;
  writeText.mockReset().mockResolvedValue(undefined);
  execCommand.mockReset().mockReturnValue(true);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  Object.defineProperty(document, "execCommand", {
    configurable: true,
    value: execCommand,
  });
});

afterEach(() => {
  document.body.replaceChildren();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(document, "execCommand");
});

describe("copyText", () => {
  it("keeps successful web copies on the asynchronous clipboard API", async () => {
    await copyText("https://preview.test/s/current");
    expect(writeText).toHaveBeenCalledWith("https://preview.test/s/current");
    expect(execCommand).not.toHaveBeenCalled();
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("copies natively during the tap without awaiting a denied or pending API", async () => {
    platform.native = true;
    writeText.mockImplementation(() => new Promise(() => {}));
    const dialog = document.createElement("div");
    const opener = document.createElement("button");
    dialog.appendChild(opener);
    document.body.appendChild(dialog);
    opener.focus();
    execCommand.mockImplementation((command: string) => {
      expect(command).toBe("copy");
      const selected = document.activeElement;
      expect(selected).toBeInstanceOf(HTMLTextAreaElement);
      if (!(selected instanceof HTMLTextAreaElement)) {
        throw new Error("Expected the copy selection");
      }
      expect(selected.parentElement).toBe(dialog);
      expect(selected.readOnly).toBe(true);
      expect(selected.value).toBe("https://www.agent-mesh.app/s/current");
      expect(selected.selectionStart).toBe(0);
      expect(selected.selectionEnd).toBe(selected.value.length);
      return true;
    });

    const result = copyText("https://www.agent-mesh.app/s/current", dialog);
    expect(execCommand).toHaveBeenCalledTimes(1);
    await result;
    expect(writeText).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(opener);
    expect(dialog.querySelector("textarea")).toBeNull();
  });

  it("falls back after a denied web write and restores the user's selection", async () => {
    writeText.mockRejectedValue(new DOMException("Write permission denied", "NotAllowedError"));
    const input = document.createElement("input");
    input.value = "retained selection";
    document.body.appendChild(input);
    input.focus();
    input.setSelectionRange(2, 8, "backward");

    await copyText("amwf:sanitized-code");

    expect(execCommand).toHaveBeenCalledWith("copy");
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(2);
    expect(input.selectionEnd).toBe(8);
    expect(input.selectionDirection).toBe("backward");
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("uses selection-copy when the web Clipboard API is absent", async () => {
    vi.stubGlobal("navigator", {});
    await copyText("amwf:sanitized-code");
    expect(execCommand).toHaveBeenCalledWith("copy");
  });

  it("can still use a permitted native async write when selection-copy is refused", async () => {
    platform.native = true;
    execCommand.mockReturnValue(false);
    await copyText("current");
    expect(writeText).toHaveBeenCalledWith("current");
  });

  it.each([false, true])("does not report success when both methods fail, native=%s", async (native) => {
    platform.native = native;
    execCommand.mockReturnValue(false);
    writeText.mockRejectedValue(new Error("Write permission denied"));
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();

    await expect(copyText("current")).rejects.toThrow("Clipboard access was blocked.");
    expect(document.activeElement).toBe(button);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("cleans up and restores focus if selection-copy throws", async () => {
    platform.native = true;
    execCommand.mockImplementation(() => {
      throw new Error("copy unavailable");
    });
    writeText.mockRejectedValue(new Error("Write permission denied"));
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();

    await expect(copyText("current")).rejects.toThrow("Clipboard access was blocked.");
    expect(document.activeElement).toBe(button);
    expect(document.querySelector("textarea")).toBeNull();
  });
});
