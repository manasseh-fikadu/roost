<!-- The name Bield goes by and the address people and agents reach it at. -->
<script lang="ts">
	import { changeSettings } from '#lib/api/client.js';
	import type { SettingsView } from '#lib/api/types.js';
	import { Draft } from '#lib/settings/draft.svelte.js';
	import FormCard from '#lib/settings/FormCard.svelte';
	import { changedFields, publicUrlProblem } from '#lib/settings.js';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	let { view, onsaved }: Props = $props();

	const draft = new Draft(() => view.settings.general);
	let saving = $state(false);

	const problem = $derived(
		draft.value.name.trim() === '' ? 'Give Bield a name.' : publicUrlProblem(draft.value.publicUrl)
	);

	async function save() {
		saving = true;
		try {
			const saved = await changeSettings({
				general: changedFields($state.snapshot(draft.value), draft.base)
			});
			draft.adopt(saved.settings.general);
			onsaved(saved);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<FormCard
	title="General"
	description="How Bield presents itself."
	dirty={draft.dirty}
	{saving}
	{problem}
	onsave={save}
	onreset={() => draft.reset()}
>
	<div class="max-w-xl space-y-4">
		<Field label="Name" hint="Shown in the sidebar and in the title of every page.">
			<Input bind:value={draft.value.name} maxlength={40} autocomplete="off" />
		</Field>
		<Field
			label="Public URL"
			hint="Where people and agents reach Bield, if that isn't the address you opened it at. Bield writes it into the agent install command and, later, into notification links. Leave it empty to use the address of each request."
		>
			<Input
				bind:value={draft.value.publicUrl}
				placeholder="https://bield.example.ts.net"
				inputmode="url"
				autocomplete="off"
				autocapitalize="off"
				spellcheck={false}
			/>
		</Field>
	</div>
</FormCard>
