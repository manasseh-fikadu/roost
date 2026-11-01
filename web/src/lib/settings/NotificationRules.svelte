<!--
	When Bield sends notifications: how often it reminds about an alert nobody
	has acknowledged, and the quiet hours it holds them back in. Which alerts a
	channel hears about is up to the channel.
-->
<script lang="ts">
	import { changeSettings } from '#lib/api/client.js';
	import type { SettingsView } from '#lib/api/types.js';
	import {
		browserTimezone,
		quietProblem,
		remindProblem,
		timezoneNames
	} from '#lib/notifications.js';
	import { changedFields } from '#lib/settings.js';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import { Draft } from './draft.svelte.js';
	import FormCard from './FormCard.svelte';
	import NumberInput from './NumberInput.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	let { view, onsaved }: Props = $props();

	const id = $props.id();

	const draft = new Draft(() => view.settings.notifications);
	let saving = $state(false);

	const problem = $derived(
		remindProblem(draft.value.remindHours) ||
			(draft.value.quiet.enabled ? quietProblem(draft.value.quiet) : '')
	);
	const zones = timezoneNames();
	const mine = browserTimezone();

	function toggleQuiet(on: boolean) {
		draft.value.quiet.enabled = on;
		// The hub's own time zone is rarely the one the admin lives in.
		if (on && draft.value.quiet.timezone === '' && mine !== '') {
			draft.value.quiet.timezone = mine;
		}
	}

	async function save() {
		saving = true;
		try {
			const edited = $state.snapshot(draft.value);
			const change = changedFields(edited, draft.base);
			const saved = await changeSettings({
				notifications: {
					remindHours: change.remindHours,
					// The hours are hidden while quiet hours are off, so whatever was typed
					// in them and left isn't sent.
					quiet: change.quiet
						? edited.quiet.enabled
							? changedFields(edited.quiet, draft.base.quiet)
							: { enabled: false }
						: undefined
				}
			});
			draft.adopt(saved.settings.notifications);
			onsaved(saved);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<FormCard
	title="When to notify"
	description="A channel hears when an alert opens, and again if it gets worse. Acknowledged or snoozed alerts stay quiet."
	dirty={draft.dirty}
	{saving}
	{problem}
	onsave={save}
	onreset={() => draft.reset()}
>
	<div class="max-w-xl space-y-5">
		<div>
			<label for="{id}-remind" class="block text-[13px] font-medium text-fg-muted">Reminders</label>
			<div class="mt-1.5 flex flex-wrap items-center gap-2 text-[13px] text-fg-muted">
				Remind every
				<NumberInput
					id="{id}-remind"
					bind:value={draft.value.remindHours}
					unit="hours"
					min={0}
					max={168}
				/>
			</div>
			<span class="mt-1.5 block text-[12px] text-fg-subtle">
				While an alert is open and nobody has acknowledged it. 0 turns reminders off.
			</span>
		</div>

		<div class="space-y-3 border-t border-line pt-5">
			<div class="flex items-center justify-between gap-4">
				<label for="{id}-quiet">
					<span class="block text-[13px] font-medium text-fg">Quiet hours</span>
					<span class="block text-[12px] text-fg-subtle">
						Bield holds notifications back, and sends the ones still due when the hours end.
					</span>
				</label>
				<Switch id="{id}-quiet" bind:checked={() => draft.value.quiet.enabled, toggleQuiet} />
			</div>
			{#if draft.value.quiet.enabled}
				<div class="grid gap-4 sm:grid-cols-2">
					<Field label="From">
						<Input type="time" bind:value={draft.value.quiet.start} />
					</Field>
					<Field label="Until">
						<Input type="time" bind:value={draft.value.quiet.end} />
					</Field>
					<div class="sm:col-span-2">
						<Field
							label="Time zone"
							hint="The times above are in this zone. Empty is the time zone of the machine Bield runs on."
						>
							<span class="flex gap-2">
								<Input
									bind:value={draft.value.quiet.timezone}
									list="{id}-zones"
									placeholder="Europe/Berlin"
									autocomplete="off"
									autocapitalize="off"
									spellcheck={false}
								/>
								{#if mine !== '' && draft.value.quiet.timezone !== mine}
									<Button onclick={() => (draft.value.quiet.timezone = mine)}>Use mine</Button>
								{/if}
							</span>
							<datalist id="{id}-zones">
								{#each zones as z (z)}
									<option value={z}></option>
								{/each}
							</datalist>
						</Field>
					</div>
				</div>
				<div class="flex items-center justify-between gap-4">
					<label for="{id}-critical" class="text-[13px] text-fg-muted">
						Still send critical alerts
					</label>
					<Switch id="{id}-critical" bind:checked={draft.value.quiet.allowCritical} />
				</div>
			{/if}
		</div>
	</div>
</FormCard>
