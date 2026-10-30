<!--
	A text file in CodeMirror, highlighted by its name. It opens read-only
	(still focusable, so search and keyboard selection work) and Edit makes it
	editable; Ctrl/Cmd-S saves. A save the server refuses because the file
	changed on disk offers to load that version or overwrite it. A Markdown
	file opens as a rendered preview, with a toggle for its source.

	The parent keys it by path, so it reads the file's text and time once, and
	keeps its own record of what is saved.
-->
<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { basicSetup, EditorView } from 'codemirror';
	import { Compartment, EditorState, type Extension, type Text as Doc } from '@codemirror/state';
	import { keymap } from '@codemirror/view';
	import { oneDark } from '@codemirror/theme-one-dark';
	import Pencil from '@lucide/svelte/icons/pencil';
	import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
	import Save from '@lucide/svelte/icons/save';
	import TextWrap from '@lucide/svelte/icons/text-wrap';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import type { FileEntry } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import { errorOf, filesApi, mtimeOf, rawUrl } from './api.js';
	import { codeTheme } from './codeTheme.js';
	import { TEXT_PARTIAL, previewKind } from './kinds.js';
	import { findLanguage, wrapsByDefault } from './language.js';
	import Markdown from './Markdown.svelte';
	import { resolveRelative } from './markdown.js';
	import { dirname } from './paths.js';

	interface Props {
		device: string;
		entry: FileEntry;
		text: string;
		/** When the file last changed, in milliseconds, as the server said when it sent the text. */
		mtime: number;
		/** Only the start or end of a large file was loaded, so it can't be edited. */
		partial: 'head' | 'tail' | null;
		ondirtychange: (dirty: boolean) => void;
		onsaved: (entry: FileEntry) => void;
		/** Drops this copy for the file as it is on disk now. */
		onreload: () => void;
	}

	let { device, entry, text, mtime, partial, ondirtychange, onsaved, onreload }: Props = $props();

	const first = untrack(() => ({ name: entry.name, text, mtime, partial }));

	/** Files past this size open without highlighting, which would stall the page. */
	const highlightLimit = 3_000_000;

	const language = findLanguage(first.name);
	const markdown = previewKind(first.name) === 'markdown';
	const modes: { value: 'preview' | 'source'; label: string }[] = [
		{ value: 'preview', label: 'Preview' },
		{ value: 'source', label: 'Source' }
	];

	// The editor changes its language, wrapping and access in place, so a toggle keeps the text,
	// the cursor, the undo history and the scroll position.
	const syntax = new Compartment();
	const wrapping = new Compartment();
	const access = new Compartment();
	const readOnly: Extension = [
		EditorState.readOnly.of(true),
		EditorView.editable.of(false),
		// A read-only view stays focusable so Ctrl-F search and keyboard selection still work.
		EditorView.contentAttributes.of({ tabindex: '0' })
	];

	let host: HTMLDivElement;
	let view: EditorView | undefined;
	/** The text as last saved or loaded, to tell whether the editor has changes. */
	let saved: Doc | undefined;
	let savedMtime = first.mtime;
	/** Set once the component is torn down, so a save that finishes late says nothing to the parent. */
	let gone = false;

	let editing = $state(false);
	let wrap = $state(wrapsByDefault(first.name));
	let dirty = $state(false);
	let saving = $state(false);
	let conflict = $state(false);
	let discarding = $state(false);
	let lines = $state<number | null>(null);
	let rendered = $state(markdown);
	/** What the preview shows: the editor's text as of when the preview was last chosen. */
	let mdText = $state(first.text);

	const showRendered = $derived(markdown && rendered && !editing);
	const summary = $derived(
		lines === null
			? bytes(entry.size)
			: `${lines.toLocaleString()} ${lines === 1 ? 'line' : 'lines'} · ${bytes(entry.size)}`
	);

	function markDirty(next: boolean) {
		if (gone || next === dirty) {
			return;
		}
		dirty = next;
		ondirtychange(next);
	}

	onMount(() => {
		const v = new EditorView({
			doc: first.text,
			parent: host,
			extensions: [
				codeTheme,
				oneDark,
				basicSetup,
				syntax.of([]),
				wrapping.of(wrap ? EditorView.lineWrapping : []),
				access.of(readOnly),
				EditorView.contentAttributes.of({ 'aria-label': first.name }),
				keymap.of([
					{
						key: 'Mod-s',
						// Even a read-only view keeps the browser from offering to save the page.
						preventDefault: true,
						run: () => {
							void save();
							return true;
						}
					}
				]),
				EditorView.updateListener.of((u) => {
					if (u.docChanged) {
						markDirty(!(saved && u.state.doc.eq(saved)));
						lines = u.state.doc.lines;
					}
				})
			]
		});
		view = v;
		saved = v.state.doc;
		lines = v.state.doc.lines;
		if (first.partial === 'tail') {
			v.dispatch({ selection: { anchor: v.state.doc.length }, scrollIntoView: true });
		}
		let live = true;
		// Grammars are separate chunks, loaded as a file needs them.
		if (language && first.text.length < highlightLimit) {
			language
				.load()
				.then((support) => live && v.dispatch({ effects: syntax.reconfigure(support) }))
				.catch(() => {});
		}
		return () => {
			live = false;
			// The parent shouldn't keep warning about changes that went away with the editor.
			if (dirty) {
				ondirtychange(false);
			}
			gone = true;
			v.destroy();
			view = undefined;
		};
	});

	// Closing the tab would lose the changes.
	$effect(() => {
		if (!dirty) {
			return;
		}
		const guard = (e: BeforeUnloadEvent) => e.preventDefault();
		window.addEventListener('beforeunload', guard);
		return () => window.removeEventListener('beforeunload', guard);
	});

	function toggleWrap() {
		wrap = !wrap;
		view?.dispatch({ effects: wrapping.reconfigure(wrap ? EditorView.lineWrapping : []) });
	}

	function setEditing(on: boolean) {
		editing = on;
		view?.dispatch({ effects: access.reconfigure(on ? [] : readOnly) });
		// The editor may be hidden behind the preview until the page updates.
		if (on || !(markdown && rendered)) {
			void tick().then(() => view?.focus());
		}
	}

	function showMode(next: 'preview' | 'source') {
		if (next === 'preview') {
			mdText = view?.state.doc.toString() ?? mdText;
		}
		rendered = next === 'preview';
	}

	async function save(force = false) {
		const v = view;
		if (!v || !editing || saving) {
			return;
		}
		const doc = v.state.doc;
		saving = true;
		try {
			const next = await filesApi.write(device, entry.path, doc.toString(), {
				expectedMtime: force ? undefined : savedMtime
			});
			savedMtime = mtimeOf(next);
			saved = doc;
			// Keys pressed during the save are still unsaved.
			markDirty(!v.state.doc.eq(doc));
			conflict = false;
			toast(`Saved ${entry.name}`);
			onsaved(next);
		} catch (err) {
			const { status, message } = errorOf(err);
			if (status === 409) {
				conflict = true;
			} else {
				toast(message, 'error');
			}
		} finally {
			saving = false;
		}
	}

	/** Done, or Discard if there are changes, which asks first. */
	function leave() {
		if (dirty) {
			discarding = true;
		} else {
			finish();
		}
	}

	function discard() {
		const v = view;
		if (v && saved) {
			v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: saved } });
		}
		discarding = false;
		finish();
	}

	function finish() {
		mdText = saved?.toString() ?? first.text;
		markDirty(false);
		conflict = false;
		setEditing(false);
	}

	/** Where a relative link in the document leads: the file it names, on the same machine. */
	function resolveLink(rel: string): string | null {
		const path = resolveRelative(dirname(entry.path), rel);
		return path === null ? null : rawUrl(device, path);
	}
</script>

<div class="flex size-full flex-col bg-surface">
	<div class="flex h-10 shrink-0 items-center gap-2 border-b border-line px-3">
		<span
			class="shrink-0 rounded border border-line-strong bg-raised px-1.5 py-px font-mono text-[11px] text-fg-muted"
		>
			{language?.name ?? 'Plain text'}
		</span>
		<span class="hidden shrink-0 num text-[11px] text-fg-faint sm:inline">{summary}</span>
		{#if editing}
			<span
				role="status"
				class={['min-w-0 truncate text-[12px]', dirty ? 'text-amber-fg' : 'text-fg-subtle']}
			>
				{dirty ? 'Unsaved changes' : 'Editing'}
			</span>
		{/if}
		<div class="ml-auto flex shrink-0 items-center gap-1.5">
			{#if markdown && !editing}
				<Segmented
					label="Markdown view"
					options={modes}
					bind:value={() => (rendered ? 'preview' : 'source'), showMode}
				/>
			{/if}
			{#if !showRendered}
				<IconButton
					size="sm"
					label="Wrap long lines"
					aria-pressed={wrap}
					class={wrap ? 'bg-hover text-fg!' : undefined}
					onclick={toggleWrap}
				>
					<TextWrap class="size-3.5" />
				</IconButton>
			{/if}
			{#if editing}
				<Button size="xs" variant="ghost" onclick={leave}>{dirty ? 'Discard' : 'Done'}</Button>
				<Button
					size="xs"
					variant="primary"
					loading={saving}
					disabled={!dirty}
					onclick={() => save()}
				>
					{#snippet icon()}<Save class="size-3" />{/snippet}
					Save
				</Button>
			{:else}
				<Button
					size="xs"
					disabled={partial !== null}
					title={partial ? 'Only part of this file is loaded' : 'Edit (Ctrl/Cmd-S saves)'}
					onclick={() => setEditing(true)}
				>
					{#snippet icon()}<Pencil class="size-3" />{/snippet}
					Edit
				</Button>
			{/if}
		</div>
	</div>
	{#if conflict}
		<div
			role="alert"
			class="flex shrink-0 flex-wrap items-center gap-2 border-b border-amber/25 bg-amber/[0.06] px-3 py-2 text-[12.5px]"
		>
			<TriangleAlert class="size-3.5 shrink-0 text-amber-fg" />
			<span class="min-w-0 flex-1 text-fg">{entry.name} changed on disk since you opened it.</span>
			<Button size="xs" variant="ghost" onclick={onreload}>
				{#snippet icon()}<RotateCcw class="size-3" />{/snippet}
				Load theirs
			</Button>
			<Button size="xs" variant="danger" loading={saving} onclick={() => save(true)}>
				Overwrite with mine
			</Button>
		</div>
	{/if}
	{#if partial}
		<div class="shrink-0 border-b border-line bg-raised px-3 py-1.5 text-[12px] text-fg-subtle">
			Showing the {partial === 'tail' ? 'last' : 'first'}
			{bytes(TEXT_PARTIAL)} of {bytes(entry.size)}, which can't be edited. Download the file to see
			all of it.
		</div>
	{/if}
	{#if showRendered}
		<div class="min-h-0 flex-1">
			<Markdown text={mdText} resolve={resolveLink} />
		</div>
	{/if}
	<div bind:this={host} class={['min-h-0 flex-1', showRendered && 'hidden']}></div>
</div>

<Dialog bind:open={discarding} title="Discard your changes?">
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (discarding = false)}>Keep editing</Button>
		<Button variant="danger" onclick={discard}>Discard</Button>
	{/snippet}
	<p class="text-[13px] text-fg-muted">
		The edits you made to {entry.name} since you opened or last saved it will be lost.
	</p>
</Dialog>
