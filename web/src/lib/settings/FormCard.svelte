<!--
	A card of settings with Discard and Save under them. Save turns on once
	something changed and nothing is wrong, and Enter in a field saves.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';

	interface Props {
		title: string;
		description?: string;
		/** Whether the form differs from what is saved. */
		dirty: boolean;
		/** Whether the change is being saved. */
		saving?: boolean;
		/** What is wrong with the form, in a sentence. It keeps Save off. */
		problem?: string;
		onsave: () => void;
		onreset: () => void;
		/** The fields. */
		children: Snippet;
		/** Words at the left of the buttons, such as what Save does. */
		note?: Snippet;
	}

	let {
		title,
		description,
		dirty,
		saving = false,
		problem = '',
		onsave,
		onreset,
		children,
		note
	}: Props = $props();

	const canSave = $derived(dirty && !problem && !saving);

	function submit(e: SubmitEvent) {
		e.preventDefault();
		if (canSave) {
			onsave();
		}
	}
</script>

<Card {title} {description}>
	<form onsubmit={submit} novalidate>
		<div class="space-y-4 p-4">{@render children()}</div>
		<div
			class="flex flex-wrap items-center justify-between gap-3 border-t border-line bg-canvas/40 px-4 py-3"
		>
			<p class="min-w-0 flex-1 text-[12.5px]" aria-live="polite">
				{#if problem && dirty}
					<span class="text-red-fg">{problem}</span>
				{:else if note}
					<span class="text-fg-subtle">{@render note()}</span>
				{/if}
			</p>
			<div class="flex items-center gap-2">
				<Button variant="ghost" disabled={!dirty || saving} onclick={onreset}>Discard</Button>
				<Button variant="primary" type="submit" loading={saving} disabled={!canSave}>
					Save changes
				</Button>
			</div>
		</div>
	</form>
</Card>
