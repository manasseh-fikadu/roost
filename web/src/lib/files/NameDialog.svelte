<!-- Asks for a name: a new folder, a new file, or what to rename something to. -->
<script lang="ts">
	import { tick } from 'svelte';
	import { errorOf } from './api.js';
	import { nameProblem } from './names.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import { fieldClass } from '#lib/ui/button.js';
	import ErrorLine from './ErrorLine.svelte';

	interface Props {
		open: boolean;
		title: string;
		description?: string;
		initial?: string;
		confirm: string;
		/** Does the work. If it throws, the dialog stays open and says why. */
		onsubmit: (name: string) => Promise<void>;
	}

	let { open = $bindable(), title, description, initial = '', confirm, onsubmit }: Props = $props();

	let name = $state('');
	let error = $state<string | null>(null);
	let busy = $state(false);
	let input = $state<HTMLInputElement>();

	$effect(() => {
		if (!open) {
			return;
		}
		name = initial;
		error = null;
		// Select the name without its extension, as a file manager does for a rename.
		void tick().then(() =>
			setTimeout(() => {
				input?.focus();
				const dot = initial.lastIndexOf('.');
				input?.setSelectionRange(0, dot > 0 ? dot : initial.length);
			}, 30)
		);
	});

	const trimmed = $derived(name.trim());
	const problem = $derived(nameProblem(trimmed));

	async function submit() {
		if (!trimmed || problem || busy) {
			return;
		}
		busy = true;
		error = null;
		try {
			await onsubmit(trimmed);
			open = false;
		} catch (err) {
			error = errorOf(err).message;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open {title} {description} width="sm:max-w-md">
	<form
		onsubmit={(e) => {
			e.preventDefault();
			void submit();
		}}
	>
		<input
			bind:this={input}
			bind:value={name}
			spellcheck="false"
			autocomplete="off"
			aria-label="Name"
			class={[fieldClass, 'h-9 font-mono text-[13px]']}
		/>
		{#if problem || error}
			<ErrorLine>{problem ?? error}</ErrorLine>
		{/if}
	</form>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button
			variant="primary"
			loading={busy}
			disabled={!trimmed || !!problem || trimmed === initial}
			onclick={submit}
		>
			{confirm}
		</Button>
	{/snippet}
</Dialog>
