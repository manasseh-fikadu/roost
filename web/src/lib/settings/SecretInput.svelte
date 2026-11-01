<!--
	A field for a secret, such as a webhook's address or a password. Bield never
	sends a saved secret back, so the field is empty and says one is saved:
	leave it empty to keep it, type to replace it. An optional secret can also
	be taken away.
-->
<script lang="ts">
	import Input from '#lib/ui/Input.svelte';
	import Button from '#lib/ui/Button.svelte';

	interface Props {
		label: string;
		/** What the user types. */
		value: string;
		/** Whether the user takes the saved secret away. */
		cleared: boolean;
		/** Whether the channel has a secret saved. */
		saved: boolean;
		/** Whether the channel works without it. */
		optional?: boolean;
		placeholder?: string;
		hint?: string;
	}

	let {
		label,
		value = $bindable(),
		cleared = $bindable(),
		saved,
		optional = false,
		placeholder,
		hint
	}: Props = $props();

	const id = $props.id();
</script>

<div>
	<label for={id} class="mb-1.5 block text-[13px] font-medium text-fg-muted">
		{label}
		{#if optional}
			<span class="font-normal text-fg-faint">(optional)</span>
		{/if}
	</label>
	<div class="flex gap-2">
		<Input
			{id}
			type="password"
			bind:value
			disabled={cleared}
			autocomplete="new-password"
			autocapitalize="off"
			spellcheck={false}
			placeholder={saved && !cleared ? 'Saved. Type to replace it.' : placeholder}
		/>
		{#if saved && optional}
			<Button variant="ghost" onclick={() => (cleared = !cleared)}>
				{cleared ? 'Keep' : 'Remove'}
			</Button>
		{/if}
	</div>
	{#if cleared}
		<span class="mt-1.5 block text-[12px] text-amber-fg">It is removed when you save.</span>
	{:else if hint}
		<span class="mt-1.5 block text-[12px] text-fg-subtle">{hint}</span>
	{/if}
</div>
