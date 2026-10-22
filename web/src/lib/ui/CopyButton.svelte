<!-- A button that copies a value to the clipboard, and says so. -->
<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import Copy from '@lucide/svelte/icons/copy';
	import { toast } from '#lib/toast.svelte.js';
	import IconButton from './IconButton.svelte';

	interface Props {
		value: string;
		/** What's copied, as in "Copy command". */
		label?: string;
	}

	let { value, label = 'Copy' }: Props = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy() {
		try {
			// The clipboard API needs a secure context, which Roost's HTTPS
			// name on the tailnet is.
			await navigator.clipboard.writeText(value);
		} catch {
			toast("Couldn't copy. Select the text and copy it yourself.", 'error');
			return;
		}
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1500);
	}
</script>

<IconButton label={copied ? 'Copied' : label} size="sm" onclick={copy}>
	{#if copied}
		<Check class="size-3.5 text-green-fg" />
	{:else}
		<Copy class="size-3.5" />
	{/if}
</IconButton>
