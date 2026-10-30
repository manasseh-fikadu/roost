<!--
	Asks an admin to confirm starting, stopping or restarting a container, and
	does it. Set `pending` to open it. The server logs what was done in the
	activity log; if it fails, a toast says why and the dialog stays open.
-->
<script lang="ts">
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import { actionCopy, actionIcons, type PendingAction } from './actions.js';
	import { runDockerAction } from './api.js';

	interface Props {
		/** The device the container is on, by ID, and its name for the dialog. */
		deviceId: string;
		deviceName: string;
		/** What to confirm, or null. It is set back to null when the dialog closes. */
		pending: PendingAction | null;
		/** Called after the action worked, to load what changed. */
		ondone?: () => void;
	}

	let { deviceId, deviceName, pending = $bindable(), ondone }: Props = $props();

	let busy = $state(false);
	// The dialog keeps what it asked while it fades out.
	let shown = $state<PendingAction | null>(null);
	let open = $state(false);

	$effect(() => {
		if (pending) {
			shown = pending;
			open = true;
		}
	});

	$effect(() => {
		if (!open) {
			pending = null;
		}
	});

	async function run() {
		if (!shown) {
			return;
		}
		busy = true;
		try {
			const res = await runDockerAction(deviceId, shown.id, shown.action);
			const copy = actionCopy[shown.action];
			toast(
				res.changed
					? `${copy.done} ${shown.name}`
					: `${shown.name} was ${shown.action === 'start' ? 'running' : 'stopped'} already`
			);
			open = false;
			ondone?.();
		} catch {
			// A toast said why.
		} finally {
			busy = false;
		}
	}
</script>

<Dialog
	bind:open
	title={shown ? `${actionCopy[shown.action].label} ${shown.name}?` : ''}
	description="On {deviceName}"
	width="sm:max-w-md"
>
	{#if shown}
		<p class="text-[13px] text-fg-muted">{actionCopy[shown.action].body}</p>
	{/if}
	{#snippet footer()}
		<Button variant="ghost" disabled={busy} onclick={() => (open = false)}>Cancel</Button>
		{#if shown}
			{@const Icon = actionIcons[shown.action]}
			<Button
				variant={shown.action === 'start' ? 'primary' : 'danger'}
				loading={busy}
				onclick={run}
			>
				{#snippet icon()}<Icon class="size-3.5" />{/snippet}
				{actionCopy[shown.action].label}
			</Button>
		{/if}
	{/snippet}
</Dialog>
