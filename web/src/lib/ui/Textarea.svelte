<!-- A text field of several lines. Give it a label, such as with <Field>. -->
<script lang="ts">
	import type { HTMLTextareaAttributes } from 'svelte/elements';
	import { fieldClass } from './button.js';

	let { value = $bindable(), class: className, ...rest }: HTMLTextareaAttributes = $props();
</script>

<textarea bind:value class={[fieldClass, 'min-h-20 py-2 leading-5', className]} {...rest}
></textarea>
