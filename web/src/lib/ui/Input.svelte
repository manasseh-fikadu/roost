<!-- A one-line text field. Give it a label, such as with <Field>. -->
<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';
	import { fieldClass } from './button.js';

	let { value = $bindable(), class: className, ...rest }: HTMLInputAttributes = $props();
</script>

<input bind:value class={[fieldClass, 'h-9', className]} {...rest} />
