/**
 * Preload of the ssh prompt window (sshPrompts.ts): fills in its static page,
 * which runs no script, and sends back the answer.
 */
import { ipcRenderer, type IpcRendererEvent } from 'electron';

/** ssh reads no more of an answer. */
const MAX_ANSWER_BYTES = 1023;
/** Until then, keys and clicks meant for the window underneath go nowhere. */
const ARMING_MS = 500;

interface SshPromptShowPayload {
  label: string;
  message: string;
  retry?: boolean;
}

ipcRenderer.once(
  'ssh-prompt:show',
  (_event: IpcRendererEvent, { label, message, retry }: SshPromptShowPayload) => {
    const [asks, text, note, long, input, ok, cancel] = [
      'asks',
      'message',
      'retry',
      'long',
      'answer',
      'continue',
      'cancel',
    ].map((id) => document.getElementById(id)) as [
      HTMLElement | null,
      HTMLElement | null,
      HTMLElement | null,
      HTMLElement | null,
      HTMLInputElement | null,
      HTMLElement | null,
      HTMLElement | null,
    ];
    if (!asks || !text || !note || !long || !input || !ok || !cancel) {
      return;
    }
    asks.textContent = `${label} asks:`;
    text.textContent = message;
    note.hidden = !retry;
    // Armed once writable: nothing typed or pressed before then counts.
    input.readOnly = true;
    setTimeout(() => (input.readOnly = false), ARMING_MS);
    const submit = (): void => {
      if (input.readOnly) {
        return;
      }
      // Refused here, where it can be fixed: ssh would read only part of it.
      long.hidden =
        new TextEncoder().encode(input.value).length <= MAX_ANSWER_BYTES;
      if (long.hidden) {
        void ipcRenderer.invoke('ssh-prompt:submit', input.value);
      }
    };
    const dismiss = (): void => void ipcRenderer.invoke('ssh-prompt:cancel');
    ok.addEventListener('click', submit);
    cancel.addEventListener('click', dismiss);
    input.addEventListener('keydown', (event: KeyboardEvent) => {
      if (event.key === 'Enter') {
        submit();
      }
    });
    document.addEventListener('keydown', (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        dismiss();
      }
    });
    input.focus();
  },
);
