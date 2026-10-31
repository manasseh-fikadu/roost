<!--
	One line of a container's log. The text goes in <span>s and is never
	parsed as HTML: the output of a container can't be trusted. The line
	number is drawn by CSS, so selecting and copying lines takes only their
	text.
-->
<script lang="ts">
	import { parseAnsi } from './ansi.js';
	import { logTime, noSearch, splitMarks, type LogEntry, type Piece, type Search } from './logs.js';

	interface Props {
		entry: LogEntry;
		wrap: boolean;
		timestamps: boolean;
		search?: Search;
	}

	let { entry, wrap, timestamps, search = noSearch }: Props = $props();

	const pieces: Piece[] = $derived.by(() => {
		// Text with no escape codes is one piece, and needs no parsing.
		const segments = entry.p === entry.m ? [{ text: entry.p, css: '' }] : parseAnsi(entry.m);
		const ranges = search.ranges ? search.ranges(entry.p) : [];
		return ranges.length > 0
			? splitMarks(segments, ranges)
			: segments.map((s) => ({ ...s, mark: false }));
	});
</script>

<div
	class={[
		'flex border-l-2 pr-3 leading-[18px] hover:bg-white/[0.03]',
		wrap ? 'min-h-[18px]' : 'h-[18px]',
		entry.s === 2 ? 'border-red bg-red/[0.06] text-red-fg' : 'border-transparent text-fg'
	]}
>
	<span
		class="w-14 shrink-0 pr-3 text-right text-fg-faint select-none before:content-[attr(data-n)]"
		data-n={entry.n}
		aria-hidden="true"
	></span>
	{#if timestamps}
		<span class="shrink-0 pr-3 text-fg-faint" title={entry.t}>{logTime(entry.t)}</span>
	{/if}
	<span
		class={wrap ? 'min-w-0 flex-1 [overflow-wrap:anywhere] whitespace-pre-wrap' : 'whitespace-pre'}
	>
		{#each pieces as p, i (i)}
			{#if p.mark}
				<mark class="rounded-[2px] bg-amber/35 text-inherit" style={p.css || undefined}
					>{p.text}</mark
				>
			{:else if p.css}
				<span style={p.css}>{p.text}</span>
			{:else}
				{p.text}
			{/if}
		{/each}
		{#if entry.cut > 0}
			<span class="text-fg-faint select-none">
				… and {entry.cut.toLocaleString()} more characters</span
			>
		{/if}
	</span>
</div>
