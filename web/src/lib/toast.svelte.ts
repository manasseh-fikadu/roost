// Short messages in the corner of the screen, such as "Check saved" or why a
// change failed. <Toaster> in the shell shows them.

export type ToastTone = 'default' | 'error';

/** A button on a toast, such as "Undo". The toast goes away when it's pressed. */
export interface ToastAction {
	label: string;
	run: () => void;
}

export interface Toast {
	id: number;
	message: string;
	tone: ToastTone;
	action?: ToastAction;
}

export interface ToastOptions {
	action?: ToastAction;
	/** How long it stays, in milliseconds, instead of the usual for its tone. */
	duration?: number;
}

/** How long a toast stays, in milliseconds. Errors stay longer, to be read. */
const durations: Record<ToastTone, number> = { default: 3200, error: 6000 };

/** The most toasts shown at once. Older ones go first. */
const maxToasts = 4;

let nextId = 1;

/** The toasts on screen now, oldest first. */
export const toasts: Toast[] = $state([]);

/** Shows a toast, and returns its id. */
export function toast(
	message: string,
	tone: ToastTone = 'default',
	{ action, duration }: ToastOptions = {}
): number {
	const t = { id: nextId++, message, tone, action };
	toasts.push(t);
	if (toasts.length > maxToasts) {
		toasts.splice(0, toasts.length - maxToasts);
	}
	setTimeout(() => dismiss(t.id), duration ?? durations[tone]);
	return t.id;
}

/** Takes a toast away before its time is up. */
export function dismiss(id: number): void {
	const i = toasts.findIndex((t) => t.id === id);
	if (i >= 0) {
		toasts.splice(i, 1);
	}
}
