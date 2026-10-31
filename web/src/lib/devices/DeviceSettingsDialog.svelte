<!--
	Where admins change a device's display name, kind, offline alerts, its own
	alert thresholds and notes.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { putDeviceSettings } from '#lib/api/client.js';
	import type { Device, DeviceKind, DeviceSettings } from '#lib/api/types.js';
	import { deviceKinds, kindLabel } from '#lib/device.js';
	import { plural } from '#lib/format.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import Textarea from '#lib/ui/Textarea.svelte';
	import { live } from '#lib/live.svelte.js';
	import NumberInput from '#lib/settings/NumberInput.svelte';
	import { draftOf, thresholdsOf, thresholdsProblem } from './thresholds.js';

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
	// A threshold that is NaN, an empty field, is the setting's.
	let diskWarn = $derived(draftOf(settings?.thresholds).diskWarn);
	let diskCritical = $derived(draftOf(settings?.thresholds).diskCritical);
	let memoryWarn = $derived(draftOf(settings?.thresholds).memoryWarn);
	let cpuWarn = $derived(draftOf(settings?.thresholds).cpuWarn);
	let offlineAfter = $derived(draftOf(settings?.thresholds).offlineMinutes);
	const own = $derived({
		diskWarn,
		diskCritical,
		memoryWarn,
		cpuWarn,
		offlineMinutes: offlineAfter
	});
	let saving = $state(false);

	const global = $derived(live.snapshot?.hub.thresholds);
	const problem = $derived(thresholdsProblem(own));
	/** How long the device can be offline before the alert, by its own threshold or the setting's. */
	const offlineMinutes = $derived(
		Number.isNaN(own.offlineMinutes) ? global?.offlineMinutes : own.offlineMinutes
	);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		try {
			await putDeviceSettings(device.id, {
				displayName: name.trim(),
				...(kind ? { kind } : {}),
				alertOffline,
				thresholds: thresholdsOf(own),
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
					{#if offlineMinutes}
						Roost raises an alert once it's been offline for {plural(offlineMinutes, 'minute')}.
					{:else}
						Roost raises an alert once it's been offline for a while.
					{/if}
				</span>
			</label>
			<Switch id="{id}-alert" bind:checked={alertOffline} />
		</div>
		<fieldset class="space-y-3 rounded-lg border border-line bg-canvas px-3 py-3">
			<legend class="px-1 text-[13px] font-medium text-fg-muted">Alert thresholds</legend>
			<p class="text-[12px] text-fg-subtle">
				Leave a field empty to use the value from Settings, which it shows in grey.
			</p>
			<div class="grid gap-3 sm:grid-cols-2">
				<Field label="Disk warning">
					<NumberInput
						bind:value={diskWarn}
						unit="%"
						min={1}
						max={100}
						placeholder={global ? String(global.diskWarn) : ''}
					/>
				</Field>
				<Field label="Disk critical">
					<NumberInput
						bind:value={diskCritical}
						unit="%"
						min={1}
						max={100}
						placeholder={global ? String(global.diskCritical) : ''}
					/>
				</Field>
				<Field label="Memory">
					<NumberInput
						bind:value={memoryWarn}
						unit="%"
						min={1}
						max={100}
						placeholder={global ? String(global.memoryWarn) : ''}
					/>
				</Field>
				<Field label="CPU">
					<NumberInput
						bind:value={cpuWarn}
						unit="%"
						min={1}
						max={100}
						placeholder={global ? String(global.cpuWarn) : ''}
					/>
				</Field>
				<Field label="Offline after">
					<NumberInput
						bind:value={offlineAfter}
						unit="minutes"
						min={1}
						max={10080}
						placeholder={global ? String(global.offlineMinutes) : ''}
					/>
				</Field>
			</div>
			{#if problem}
				<p class="text-[12.5px] text-red-fg" role="alert">{problem}</p>
			{/if}
		</fieldset>
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
		<Button variant="primary" type="submit" form="{id}-form" loading={saving} disabled={!!problem}>
			Save
		</Button>
	{/snippet}
</Dialog>
