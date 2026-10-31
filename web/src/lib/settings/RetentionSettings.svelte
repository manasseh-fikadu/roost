<!--
	How many days Roost keeps each kind of history. Keeping something for fewer
	days than before deletes data, so that asks first.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { changeSettings } from '#lib/api/client.js';
	import type { RetentionSettings, SettingsView } from '#lib/api/types.js';
	import { retention, retentionProblem, same, shortensRetention } from '#lib/settings.js';
	import FormCard from '#lib/settings/FormCard.svelte';
	import NumberInput from '#lib/settings/NumberInput.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	/** What the kind of history is, where it needs more words than its description. */
	const hints: Partial<Record<keyof RetentionSettings, string>> = {
		historyDays: 'The device pages chart the last 7 days; longer history arrives with rollups.'
	};

	let { view, onsaved }: Props = $props();

	const id = $props.id();

	// The form starts from what is saved and stays as it is until it is saved
	// or discarded, so another section saving doesn't wipe a half-edited one.
	let draft: RetentionSettings = $state(untrack(() => ({ ...view.settings.retention })));
	let saving = $state(false);

	// The kinds of history that would shrink, as of when Save was pressed, so
	// that the dialog keeps saying so while it fades out.
	let confirming = $state(false);
	let shrinking: { label: string; from: number; to: number }[] = $state([]);

	const dirty = $derived(!same(draft, view.settings.retention));
	const problem = $derived(retentionProblem(draft));
	const differsFromDefaults = $derived(!same(draft, view.defaults.retention));

	function days(n: number): string {
		return n === 1 ? '1 day' : `${n} days`;
	}

	function reset() {
		draft = { ...view.settings.retention };
	}

	function restoreDefaults() {
		draft = { ...view.defaults.retention };
	}

	function submit() {
		const saved = view.settings.retention;
		if (!shortensRetention(saved, draft)) {
			void save();
			return;
		}
		shrinking = retention
			.filter(({ key }) => draft[key] < saved[key])
			.map(({ key, label }) => ({ label, from: saved[key], to: draft[key] }));
		confirming = true;
	}

	async function save() {
		saving = true;
		try {
			const saved = await changeSettings({ retention: $state.snapshot(draft) });
			onsaved(saved);
			draft = { ...saved.settings.retention };
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
	{dirty}
	{saving}
	{problem}
	onsave={submit}
	onreset={reset}
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
					bind:value={draft[info.key]}
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
