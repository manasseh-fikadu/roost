// How the editor looks: Roost's colors over CodeMirror's One Dark syntax colors. It reads the
// theme's CSS variables (layout.css), so the editor follows the rest of the page.

import { EditorView } from '@codemirror/view';

export const codeTheme = EditorView.theme(
	{
		'&': {
			height: '100%',
			fontSize: '12.5px',
			backgroundColor: 'var(--color-surface)',
			color: 'var(--color-fg)'
		},
		'&.cm-focused': { outline: 'none' },
		'.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.65' },
		'.cm-content': { padding: '10px 0' },
		'.cm-gutters': {
			backgroundColor: 'var(--color-surface)',
			color: 'var(--color-fg-faint)',
			borderRight: '1px solid var(--color-line)'
		},
		'.cm-lineNumbers .cm-gutterElement': { padding: '0 10px 0 14px' },
		'.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--color-fg-muted)' },
		'.cm-activeLine': { backgroundColor: 'rgb(255 255 255 / 0.03)' },
		'&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection':
			{ backgroundColor: 'rgb(82 168 255 / 0.28) !important' },
		'.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--color-fg)' },
		'.cm-panels': { backgroundColor: 'var(--color-raised)', color: 'var(--color-fg)' },
		'.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--color-line-strong)' },
		'.cm-panels.cm-panels-bottom': { borderTop: '1px solid var(--color-line-strong)' },
		'.cm-search': { fontFamily: 'var(--font-sans)', fontSize: '12.5px', padding: '6px 10px' },
		'.cm-textfield': {
			backgroundColor: 'var(--color-canvas)',
			border: '1px solid var(--color-line-strong)',
			borderRadius: '6px',
			color: 'var(--color-fg)',
			padding: '3px 8px'
		},
		'.cm-button': {
			backgroundImage: 'none',
			backgroundColor: 'var(--color-hover)',
			border: '1px solid var(--color-line-strong)',
			borderRadius: '6px',
			color: 'var(--color-fg)',
			padding: '3px 10px'
		},
		'.cm-searchMatch': {
			backgroundColor: 'rgb(255 178 36 / 0.22)',
			outline: '1px solid rgb(255 178 36 / 0.45)'
		},
		'.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'rgb(255 178 36 / 0.45)' },
		'.cm-selectionMatch': { backgroundColor: 'rgb(82 168 255 / 0.14)' },
		'.cm-foldPlaceholder': {
			backgroundColor: 'var(--color-hover)',
			border: '1px solid var(--color-line-strong)',
			color: 'var(--color-fg-muted)'
		},
		'.cm-tooltip': {
			backgroundColor: 'var(--color-raised)',
			border: '1px solid var(--color-line-strong)'
		}
	},
	{ dark: true }
);
