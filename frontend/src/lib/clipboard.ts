import { IS_NATIVE } from "./nativeAuth";

function copySelection(text: string, container: HTMLElement): boolean {
  const previous = document.activeElement;
  const inputSelection =
    previous instanceof HTMLInputElement || previous instanceof HTMLTextAreaElement
      ? {
          start: previous.selectionStart,
          end: previous.selectionEnd,
          direction: previous.selectionDirection,
        }
      : null;
  const selection = window.getSelection();
  const ranges: Range[] = [];
  if (selection) {
    for (let index = 0; index < selection.rangeCount; index++) {
      ranges.push(selection.getRangeAt(index).cloneRange());
    }
  }

  const field = document.createElement("textarea");
  field.value = text;
  field.readOnly = true;
  field.tabIndex = -1;
  field.setAttribute("aria-hidden", "true");
  Object.assign(field.style, {
    position: "fixed",
    top: "0",
    left: "0",
    width: "1px",
    height: "1px",
    opacity: "0",
    padding: "0",
    border: "0",
    fontSize: "16px",
  });

  try {
    container.appendChild(field);
    field.focus({ preventScroll: true });
    field.select();
    field.setSelectionRange(0, text.length);
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    field.remove();
    if (previous instanceof HTMLElement && previous.isConnected) {
      previous.focus({ preventScroll: true });
      if (
        inputSelection?.start != null &&
        inputSelection.end != null &&
        (previous instanceof HTMLInputElement ||
          previous instanceof HTMLTextAreaElement)
      ) {
        previous.setSelectionRange(
          inputSelection.start,
          inputSelection.end,
          inputSelection.direction ?? undefined,
        );
      }
    }
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
  }
}

export async function copyText(
  text: string,
  container: HTMLElement = document.body,
): Promise<void> {
  // Android WebView can expose writeText while denying every write. Copy
  // synchronously during the native tap before trying that asynchronous API.
  if (IS_NATIVE && copySelection(text, container)) return;

  try {
    if (!navigator.clipboard?.writeText) throw new Error("Clipboard unavailable");
    await navigator.clipboard.writeText(text);
  } catch {
    if (!IS_NATIVE && copySelection(text, container)) return;
    throw new Error("Clipboard access was blocked.");
  }
}
