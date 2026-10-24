<!-- An alert's notes, and for admins, a box to add one. -->
<script lang="ts">
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { addAlertNote } from '#lib/api/client.js';
	import type { Alert, AlertNote } from '#lib/api/types.js';
	import { dateTime } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Textarea from '#lib/ui/Textarea.svelte';

	interface Props {
		alert: Alert;
		open?: boolean;
	}

	let { alert, open = $bindable(false) }: Props = $props();

	const id = $props.id();
	let text = $state('');
	let saving = $state(false);
	// Notes added here, until the alert has them. A resolved alert isn't in
	// the snapshots, so its notes don't come back by themselves.
	let added = $state<AlertNote[]>([]);
	const notes = $derived([
		...alert.notes,
		...added.filter((n) => !alert.notes.some((m) => m.at === n.at && m.text === n.text))
	]);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!text.trim()) {
			return;
		}
		saving = true;
		try {
			added.push(await addAlertNote(alert.id, text));
			text = '';
		} catch {
			// addAlertNote showed the error.
		} finally {
			saving = false;
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
			(e.currentTarget as HTMLTextAreaElement).form?.requestSubmit();
		}
	}
</script>

<Dialog bind:open title="Notes" description={alert.title}>
	<div class="space-y-3">
		{#each notes as n, i (i)}
			<div class="rounded-lg border border-line bg-canvas px-3 py-2.5">
				<div class="mb-1 flex items-center gap-2 font-mono text-[11px] text-fg-faint">
					{#if n.by}
						<span class="text-fg-subtle">{n.by}</span>
					{/if}
					<span>{dateTime(n.at)}</span>
				</div>
				<p class="text-[13px] whitespace-pre-wrap text-fg">{n.text}</p>
			</div>
		{:else}
			<p class="text-[13px] text-fg-subtle">
				No notes yet. Write down what you found, or what you changed.
			</p>
		{/each}
		<AdminOnly>
			<form id="{id}-form" onsubmit={save}>
				<Textarea
					bind:value={text}
					placeholder="Add a note…"
					maxlength={4000}
					aria-label="New note"
					{onkeydown}
				/>
			</form>
		</AdminOnly>
	</div>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Close</Button>
		<AdminOnly>
			<Button
				variant="primary"
				type="submit"
				form="{id}-form"
				loading={saving}
				disabled={!text.trim()}
			>
				Add note
			</Button>
		</AdminOnly>
	{/snippet}
</Dialog>
