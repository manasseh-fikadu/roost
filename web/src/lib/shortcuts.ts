// The keyboard shortcuts that work on every page, and the list `?` shows.
// Shortcuts that belong to one page, such as the file manager's, are handled
// by that page and only listed here.

/** Something a key press can be aimed at, such as a DOM element. */
export interface KeyTarget {
	closest(selector: string): unknown;
}

/**
 * Whether a key press is meant for a field or an editor, where the letters
 * are text and not shortcuts. The terminal counts: xterm listens on a hidden
 * textarea.
 */
export function isTyping(target: KeyTarget | null | undefined): boolean {
	return !!target?.closest(
		'input, textarea, select, [contenteditable]:not([contenteditable="false"]), .cm-editor'
	);
}

/** How long, in milliseconds, after `g` the page number can still come. */
export const chordTimeout = 1500;

export interface ChordStep {
	/** When `g` was pressed, if the next key can still finish the chord. */
	armedAt: number | null;
	/** The page number to go to, 1 to 9, if the chord is done. */
	page?: number;
}

/**
 * Takes one key press on the way to `g` and a page number, such as `g` `2`
 * for the second page of the sidebar. `armedAt` is what the last step gave.
 */
export function stepChord(armedAt: number | null, key: string, now: number): ChordStep {
	const armed = armedAt !== null && now - armedAt <= chordTimeout;
	if (armed && /^[1-9]$/.test(key)) {
		return { armedAt: null, page: Number(key) };
	}
	// Another `g` starts over, and anything else gives up.
	return { armedAt: key === 'g' ? now : null };
}

/** One shortcut: the keys to press, and what they do. */
export interface Shortcut {
	keys: string[];
	/** The keys are pressed one after the other, not together. */
	sequence?: boolean;
	label: string;
}

export interface ShortcutGroup {
	heading: string;
	shortcuts: Shortcut[];
}

/** The shortcuts to list. `mod` is what this computer calls the command key: `⌘` or `Ctrl`. */
export function shortcutGroups(mod: string): ShortcutGroup[] {
	return [
		{
			heading: 'Anywhere',
			shortcuts: [
				{ keys: [mod, 'K'], label: 'Search pages, devices and actions' },
				{ keys: ['?'], label: 'Show this list' },
				{ keys: ['g', '1–9'], sequence: true, label: 'Go to the page with that number' },
				{ keys: ['Esc'], label: 'Close a dialog or menu' }
			]
		},
		{
			heading: 'Files',
			shortcuts: [
				{ keys: ['↑', '↓'], label: 'Move through the list' },
				{ keys: ['Enter'], label: 'Open a file or folder' },
				{ keys: ['Space'], label: 'Select or unselect' },
				{ keys: ['Backspace'], label: 'Up one folder' },
				{ keys: ['/'], label: 'Search this folder' },
				{ keys: [mod, 'L'], label: 'Edit the path' },
				{ keys: ['F2'], label: 'Rename' },
				{ keys: ['Delete'], label: 'Delete' },
				{ keys: [mod, 'A'], label: 'Select everything' },
				{ keys: ['←', '→'], label: 'Previous or next file in a preview' }
			]
		},
		{
			heading: 'Terminal',
			shortcuts: [
				{ keys: ['Ctrl', 'Shift', 'C'], label: 'Copy the selection (Ctrl+C goes to the shell)' }
			]
		},
		{
			heading: 'Charts',
			shortcuts: [
				{ keys: ['←', '→'], label: 'Move along the chart' },
				{ keys: ['Enter'], label: 'See what was running at that moment' }
			]
		}
	];
}
