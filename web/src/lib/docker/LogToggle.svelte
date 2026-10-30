<!-- A toolbar button that is on or off, such as Wrap. Screen readers hear it as pressed or not. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		pressed: boolean;
		/** The button's name for screen readers, if its content doesn't say it. */
		label?: string;
		title?: string;
		disabled?: boolean;
		children: Snippet;
	}

	let { pressed = $bindable(), label, title, disabled = false, children }: Props = $props();
</script>

<button
	type="button"
	aria-pressed={pressed}
	aria-label={label}
	{title}
	{disabled}
	onclick={() => (pressed = !pressed)}
	class={[
		'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-[12.5px] font-medium transition-colors disabled:pointer-events-none disabled:opacity-50',
		pressed
			? 'border-line-strong bg-hover text-fg'
			: 'border-line bg-transparent text-fg-subtle hover:text-fg-muted'
	]}
>
	{@render children()}
</button>
