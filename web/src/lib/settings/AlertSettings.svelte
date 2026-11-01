<!--
	Which alerts Bield raises, and the thresholds that open them. The rules and
	the thresholds are saved together, since the rules are what the thresholds
	are for.
-->
<script lang="ts">
	import { changeSettings } from '#lib/api/client.js';
	import type { AlertRules, SettingsView, Thresholds } from '#lib/api/types.js';
	import { Draft } from '#lib/settings/draft.svelte.js';
	import FormCard from '#lib/settings/FormCard.svelte';
	import NumberInput from '#lib/settings/NumberInput.svelte';
	import { changedFields, rules, same, thresholdsProblem } from '#lib/settings.js';
	import Button from '#lib/ui/Button.svelte';
	import Switch from '#lib/ui/Switch.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	/** One threshold field of a rule, with the words before it and its unit after it. */
	interface Part {
		field: keyof Thresholds;
		before: string;
		unit: string;
		/** Names the field for screen readers. */
		label: string;
		min: number;
		max: number;
		step?: number;
	}

	/** A percentage can have a decimal, such as 99.5, so the field allows one. */
	const percent = { min: 0.1, max: 100, step: 0.1, unit: '%' };

	/** The threshold fields of the rules that have any, in the order they read. */
	const parts: Partial<Record<keyof AlertRules, Part[]>> = {
		offline: [
			{
				field: 'offlineMinutes',
				before: 'after',
				unit: 'minutes',
				label: 'Minutes offline before an alert',
				min: 1,
				max: 7 * 24 * 60
			}
		],
		keyExpiry: [
			{
				field: 'keyExpiryDays',
				before: 'when',
				unit: 'days are left',
				label: 'Days left before the key expires',
				min: 1,
				max: 365
			}
		],
		disk: [
			{ field: 'diskWarn', before: 'warn at', label: 'Disk warning threshold', ...percent },
			{ field: 'diskCritical', before: 'critical at', label: 'Disk critical threshold', ...percent }
		],
		memory: [{ field: 'memoryWarn', before: 'at', label: 'Memory threshold', ...percent }],
		cpu: [
			{ field: 'cpuWarn', before: 'at', label: 'CPU threshold', ...percent },
			{
				field: 'cpuWindowMinutes',
				before: 'for',
				unit: 'minutes',
				label: 'Minutes the CPU stays busy',
				min: 1,
				max: 60
			}
		],
		checks: [
			{
				field: 'checkFailures',
				before: 'after',
				unit: 'failures in a row',
				label: 'Failures in a row before an alert',
				min: 1,
				max: 100
			}
		]
	};

	let { view, onsaved }: Props = $props();

	const id = $props.id();

	const draft = new Draft(() => ({
		thresholds: view.settings.thresholds,
		rules: view.settings.rules
	}));
	let saving = $state(false);

	const problem = $derived(thresholdsProblem(draft.value.thresholds));
	const differsFromDefaults = $derived(
		!same(draft.value.thresholds, view.defaults.thresholds) ||
			!same(draft.value.rules, view.defaults.rules)
	);

	function restoreDefaults() {
		draft.value = {
			thresholds: { ...view.defaults.thresholds },
			rules: { ...view.defaults.rules }
		};
	}

	async function save() {
		saving = true;
		try {
			const edited = $state.snapshot(draft.value);
			const saved = await changeSettings({
				thresholds: changedFields(edited.thresholds, draft.base.thresholds),
				rules: changedFields(edited.rules, draft.base.rules)
			});
			draft.adopt({ thresholds: saved.settings.thresholds, rules: saved.settings.rules });
			onsaved(saved);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<FormCard
	title="Alert rules"
	description="Bield raises an alert when a rule's condition is true. Turning a rule off hides it from the dashboard and resolves its open alerts."
	dirty={draft.dirty}
	{saving}
	{problem}
	onsave={save}
	onreset={() => draft.reset()}
>
	<ul class="divide-y divide-line">
		{#each rules as rule (rule.key)}
			{@const fields = parts[rule.key]}
			{@const on = draft.value.rules[rule.key]}
			<li
				class="grid grid-cols-[auto_1fr] items-start gap-x-3 gap-y-2 py-3 first:pt-0 last:pb-0 lg:grid-cols-[auto_1fr_auto] lg:items-center"
			>
				<div class="mt-0.5 lg:mt-0">
					<Switch
						id="{id}-{rule.key}"
						label={rule.label}
						bind:checked={draft.value.rules[rule.key]}
					/>
				</div>
				<label for="{id}-{rule.key}" class="min-w-0">
					<span class="block text-[13px] font-medium text-fg">{rule.label}</span>
					<span class="block text-[12px] text-fg-subtle">{rule.description}</span>
				</label>
				{#if fields}
					<div
						class={[
							'col-start-2 flex flex-wrap items-center gap-x-3 gap-y-2 text-[13px] lg:col-start-3',
							on ? 'text-fg-muted' : 'text-fg-faint'
						]}
					>
						{#each fields as part (part.field)}
							<span class="flex items-center gap-2">
								{part.before}
								<NumberInput
									bind:value={draft.value.thresholds[part.field]}
									unit={part.unit}
									min={part.min}
									max={part.max}
									step={part.step}
									label={part.label}
									disabled={!on}
								/>
							</span>
						{/each}
					</div>
				{/if}
			</li>
		{/each}
	</ul>
	{#if differsFromDefaults}
		<div>
			<Button variant="ghost" size="xs" onclick={restoreDefaults}>Restore defaults</Button>
		</div>
	{/if}
	{#snippet note()}
		A device can have thresholds of its own: edit it on its page.
	{/snippet}
</FormCard>
