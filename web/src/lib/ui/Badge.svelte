<!-- A small rounded label in one of the theme's tones. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { toneClass, type Tone } from '#lib/status.js';

	interface Props {
		tone?: Tone;
		/** Monospaced capitals, as in status pills. */
		mono?: boolean;
		children: Snippet;
		class?: string;
	}

	let { tone = 'gray', mono = false, children, class: className }: Props = $props();
</script>

<span
	class={[
		'inline-flex h-5 shrink-0 items-center gap-1 rounded-full border px-2 text-[11px] leading-none font-medium whitespace-nowrap',
		mono && 'font-mono text-[10px] tracking-wide',
		toneClass[tone],
		className
	]}
>
	{@render children()}
</span>
