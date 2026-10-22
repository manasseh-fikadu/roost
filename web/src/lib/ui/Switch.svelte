<!-- An on/off switch. Name it with `label`, or point a <label for> at its `id`. -->
<script lang="ts">
	import { Switch } from 'bits-ui';

	interface Props {
		checked?: boolean;
		label?: string;
		id?: string;
		disabled?: boolean;
	}

	let { checked = $bindable(false), label, id, disabled }: Props = $props();
</script>

<Switch.Root
	bind:checked
	{id}
	{disabled}
	aria-label={label}
	class="inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-line-strong bg-hover transition-colors disabled:opacity-50 data-[state=checked]:border-blue data-[state=checked]:bg-blue"
>
	<Switch.Thumb
		class="pointer-events-none block size-3.5 translate-x-[2px] rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-[18px]"
	/>
</Switch.Root>
