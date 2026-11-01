<!-- The browser's own drop-down list, in Bield's style. Give it a label, such as with <Field>. -->
<script lang="ts">
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import type { HTMLSelectAttributes } from 'svelte/elements';
	import { fieldClass } from './button.js';

	let { value = $bindable(), class: className, children, ...rest }: HTMLSelectAttributes = $props();
</script>

<span class="relative block">
	<select bind:value class={[fieldClass, 'h-9 appearance-none pr-8', className]} {...rest}>
		{@render children?.()}
	</select>
	<ChevronDown
		class="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-fg-subtle"
	/>
</span>
