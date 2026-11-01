<!--
	How many days Roost keeps each kind of history. Keeping something for fewer
	days than before deletes data, so that asks first.
-->
<script lang="ts">
	import { changeSettings } from '#lib/api/client.js';
	import type { RetentionSettings, SettingsView } from '#lib/api/types.js';
	import { Draft } from '#lib/settings/draft.svelte.js';
	import FormCard from '#lib/settings/FormCard.svelte';
	import NumberInput from '#lib/settings/NumberInput.svelte';
	import {
		changedFields,
		retention,
		retentionProblem,
		same,
		shortensRetention
	} from '#lib/settings.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	/** What the kind of history is, where it needs more words than its description. */
	const hints: Partial<Record<keyof RetentionSettings, string>> = {
		historyDays:
			"The last 2 days are kept in full. Older history is kept as averages of 5 minutes, for up to 14 days, and of an hour. What used a device's CPU, and what its containers used, goes back 7 days at most."
	};

	let { view, onsaved }: Props = $props();

	const id = $props.id();

	const draft = new Draft(() => view.settings.retention);
	let saving = $state(false);

	// The kinds of history that would shrink, as of when Save was pressed, so
	// that the dialog keeps saying so while it fades out.
	let confirming = $state(false);
	let shrinking: { label: string; from: number; to: number }[] = $state([]);

	const problem = $derived(retentionProblem(draft.value));
	const differsFromDefaults = $derived(!same(draft.value, view.defaults.retention));

	function days(n: number): string {
		return n === 1 ? '1 day' : `${n} days`;
	}

	function restoreDefaults() {
		draft.value = { ...view.defaults.retention };
	}

	function submit() {
		const saved = view.settings.retention;
		if (!shortensRetention(saved, draft.value)) {
			void save();
			return;
		}
		shrinking = retention
			.filter(({ key }) => draft.value[key] < saved[key])
			.map(({ key, label }) => ({ label, from: saved[key], to: draft.value[key] }));
		// The dialog gives focus back to what had it when it opened. That would
		// be the Save button, which is off once the change is saved, so unless
		// the admin is in a field already, start from the first one.
		if (!(document.activeElement instanceof HTMLInputElement)) {
			document.getElementById(`${id}-${retention[0].key}`)?.focus();
		}
		confirming = true;
	}

	async function save() {
		saving = true;
		try {
			const saved = await changeSettings({
				retention: changedFields($state.snapshot(draft.value), draft.base)
			});
			draft.adopt(saved.settings.retention);
			onsaved(saved);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
			confirming = false;
		}
	}
</script>

<FormCard
	title="Retention"
	description="How long Roost keeps history. It deletes older data within the hour."
	dirty={draft.dirty}
	{saving}
	{problem}
	onsave={submit}
	onreset={() => draft.reset()}
>
	<ul class="divide-y divide-line">
		{#each retention as info (info.key)}
			<li
				class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0"
			>
				<div class="min-w-0 flex-1 basis-56">
					<label for="{id}-{info.key}" class="block text-[13px] font-medium text-fg">
						{info.label}
					</label>
					<p class="text-[12px] text-fg-subtle">{info.description}</p>
					{#if hints[info.key]}
						<p class="mt-1 text-[12px] text-fg-subtle">{hints[info.key]}</p>
					{/if}
				</div>
				<NumberInput
					id="{id}-{info.key}"
					bind:value={draft.value[info.key]}
					unit="days"
					min={1}
					max={info.max}
				/>
			</li>
		{/each}
	</ul>
	{#if differsFromDefaults}
		<div>
			<Button variant="ghost" size="xs" onclick={restoreDefaults}>Restore defaults</Button>
		</div>
	{/if}
</FormCard>

<Dialog
	bind:open={confirming}
	title="Delete older history?"
	description="Roost will delete history older than the new limits within the hour. You can't undo this."
>
	<ul class="space-y-1 text-[13px] text-fg-muted">
		{#each shrinking as kind (kind.label)}
			<li>
				<span class="font-medium text-fg">{kind.label}</span>: {days(kind.from)} to {days(kind.to)}
			</li>
		{/each}
	</ul>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (confirming = false)}>Cancel</Button>
		<Button variant="danger" loading={saving} onclick={save}>Delete older history</Button>
	{/snippet}
</Dialog>
