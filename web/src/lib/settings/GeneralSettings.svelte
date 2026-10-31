<!-- The name Roost goes by and the address people and agents reach it at. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { changeSettings } from '#lib/api/client.js';
	import type { GeneralSettings, SettingsView } from '#lib/api/types.js';
	import { publicUrlProblem, same } from '#lib/settings.js';
	import FormCard from '#lib/settings/FormCard.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	let { view, onsaved }: Props = $props();

	// The form starts from what is saved and stays as it is until it is saved
	// or discarded, so another section saving doesn't wipe a half-edited one.
	let draft: GeneralSettings = $state(untrack(() => ({ ...view.settings.general })));
	let saving = $state(false);

	const dirty = $derived(!same(draft, view.settings.general));
	const problem = $derived(
		draft.name.trim() === '' ? 'Give Roost a name.' : publicUrlProblem(draft.publicUrl)
	);

	function reset() {
		draft = { ...view.settings.general };
	}

	async function save() {
		saving = true;
		try {
			const saved = await changeSettings({ general: $state.snapshot(draft) });
			onsaved(saved);
			draft = { ...saved.settings.general };
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<FormCard
	title="General"
	description="How Roost presents itself."
	{dirty}
	{saving}
	{problem}
	onsave={save}
	onreset={reset}
>
	<div class="max-w-xl space-y-4">
		<Field label="Name" hint="Shown in the sidebar and in the title of every page.">
			<Input bind:value={draft.name} maxlength={40} autocomplete="off" />
		</Field>
		<Field
			label="Public URL"
			hint="Where people and agents reach Roost, if that isn't the address you opened it at. Roost writes it into the agent install command and, later, into notification links. Leave it empty to use the address of each request."
		>
			<Input
				bind:value={draft.publicUrl}
				placeholder="https://roost.example.ts.net"
				inputmode="url"
				autocomplete="off"
				autocapitalize="off"
				spellcheck={false}
			/>
		</Field>
	</div>
</FormCard>
