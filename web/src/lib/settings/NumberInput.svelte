<!--
	A number field with its unit after it, such as 85 %. The value is NaN while
	the field is empty or isn't a number, so the form can say what's wrong.
-->
<script lang="ts">
	import Input from '#lib/ui/Input.svelte';

	interface Props {
		value: number;
		/** What the number counts, shown after it, such as "%" or "minutes". */
		unit: string;
		min?: number;
		max?: number;
		step?: number;
		id?: string;
		disabled?: boolean;
		/** Shown while the field is empty. */
		placeholder?: string;
		/** Names the field for screen readers when no <label> does. */
		label?: string;
		class?: string;
	}

	let {
		value = $bindable(),
		unit,
		min,
		max,
		step = 1,
		id,
		disabled,
		placeholder,
		label,
		class: className
	}: Props = $props();

	function input(e: Event & { currentTarget: HTMLInputElement }) {
		value = e.currentTarget.valueAsNumber;
	}
</script>

<span class={['flex items-center gap-2', className]}>
	<Input
		type="number"
		inputmode="decimal"
		class="w-24 num"
		{id}
		{min}
		{max}
		{step}
		{disabled}
		{placeholder}
		aria-label={label}
		value={Number.isNaN(value) ? '' : value}
		oninput={input}
	/>
	<span class="text-[13px] whitespace-nowrap text-fg-subtle">{unit}</span>
</span>
