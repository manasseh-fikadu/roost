<!-- Where admins change a device's display name, kind, offline alerts and notes. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { putDeviceSettings } from '#lib/api/client.js';
	import type { Device, DeviceKind, DeviceSettings } from '#lib/api/types.js';
	import { deviceKinds, kindLabel } from '#lib/device.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import Textarea from '#lib/ui/Textarea.svelte';

	interface Props {
		device: Device;
		open?: boolean;
	}

	let { device, open = $bindable(false) }: Props = $props();

	const id = $props.id();

	// The form starts from the device's settings each time the dialog opens,
	// and stays put while it's open, even as new snapshots come, and while it
	// fades out.
	let opened: DeviceSettings | null = null;
	const settings = $derived.by(() => {
		if (open) {
			// A copy, so that opening again starts afresh even if no new
			// snapshot came in between.
			opened = { ...untrack(() => device.settings) };
		}
		return opened;
	});
	let name = $derived(settings?.displayName ?? '');
	let kind: DeviceKind | '' = $derived(settings?.kind ?? '');
	let alertOffline = $derived(settings?.alertOffline ?? true);
	let notes = $derived(settings?.notes ?? '');
	let saving = $state(false);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		try {
			await putDeviceSettings(device.id, {
				displayName: name.trim(),
				...(kind ? { kind } : {}),
				alertOffline,
				notes
			});
			open = false;
		} catch {
			// putDeviceSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<Dialog
	bind:open
	title="Device settings"
	description="{device.hostName || device.name} · {device.dnsName}"
>
	<form id="{id}-form" class="space-y-4" onsubmit={save}>
		<Field label="Display name" hint="Leave it empty to use the name the machine reports.">
			<Input bind:value={name} placeholder={device.hostName} maxlength={64} />
		</Field>
		<Field label="Type">
			<Select bind:value={kind}>
				<option value="">Detect automatically</option>
				{#each deviceKinds as k (k)}
					<option value={k}>{kindLabel(k)}</option>
				{/each}
			</Select>
		</Field>
		<div
			class="flex items-center justify-between gap-4 rounded-lg border border-line bg-canvas px-3 py-2.5"
		>
			<label for="{id}-alert">
				<span class="block text-[13px] font-medium text-fg">Alert when offline</span>
				<span class="block text-[12px] text-fg-subtle">
					Raise an alert when it's been off the tailnet for 10 minutes.
				</span>
			</label>
			<Switch id="{id}-alert" bind:checked={alertOffline} />
		</div>
		<Field label="Notes" hint="Viewers can read these too.">
			<Textarea
				bind:value={notes}
				placeholder="Where it lives, what runs on it…"
				maxlength={4000}
			/>
		</Field>
	</form>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button variant="primary" type="submit" form="{id}-form" loading={saving}>Save</Button>
	{/snippet}
</Dialog>
