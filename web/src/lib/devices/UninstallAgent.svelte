<!--
	A button that removes a device's agent over SSH, after the admin confirms,
	for the user Bield logs in as. The agent tells Bield to forget it, so the
	device stops showing one when it works. Only admins can use it.
-->
<script lang="ts">
	import { uninstallAgentOverSSH } from '#lib/api/client.js';
	import type { AgentRun, Device } from '#lib/api/types.js';
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';

	interface Props {
		/** A device that Bield can log in to. */
		device: Device;
	}

	let { device }: Props = $props();

	let open = $state(false);
	let busy = $state(false);
	/** What the uninstall printed, if it failed. */
	let failed = $state<AgentRun | null>(null);

	function ask() {
		failed = null;
		open = true;
	}

	async function uninstall() {
		busy = true;
		try {
			const ran = await uninstallAgentOverSSH(device.id);
			if (ran.ok) {
				// The agent is gone from the device, and so may this button.
				open = false;
				toast(`Removed the agent from ${device.name}`);
			} else {
				failed = ran;
			}
		} catch {
			// The client showed the error as a toast.
		} finally {
			busy = false;
		}
	}
</script>

<Button size="xs" variant="ghost" onclick={ask}>Uninstall</Button>

<Dialog
	bind:open
	title="Uninstall {device.name}'s agent?"
	description="Bield logs in to {device.name} as {device.ssh?.user ??
		'its SSH user'} and removes the agent from that user's account."
>
	<p class="text-[13px] text-fg-muted">
		The agent stops and tells Bield to forget it. If it was installed by another user, run the
		uninstall command on the machine as that user instead.
	</p>
	{#if failed}
		<p class="mt-3 text-[13px] text-red-fg" role="alert">The uninstall failed.</p>
		{#if failed.output}
			<pre
				class="mt-2 max-h-48 overflow-auto rounded-md border border-line bg-canvas px-3 py-2 font-mono text-[11.5px] whitespace-pre-wrap text-fg-muted">{failed.output}</pre>
		{/if}
	{/if}
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button variant="danger" loading={busy} onclick={uninstall}>
			{failed ? 'Try again' : 'Uninstall'}
		</Button>
	{/snippet}
</Dialog>
