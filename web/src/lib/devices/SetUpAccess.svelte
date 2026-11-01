<!--
	What an empty state offers when no machine has SSH access: set up the next
	one that can have it, or look at the devices. Only admins can set access
	up, so a page that viewers see wraps this in <AdminOnly>.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import type { Device } from '#lib/api/types.js';
	import { sshCandidate } from '#lib/setup.js';
	import { buttonClass } from '#lib/ui/button.js';

	interface Props {
		/** Every device on the tailnet. */
		devices: readonly Device[];
	}

	let { devices }: Props = $props();

	const next = $derived(sshCandidate(devices));
</script>

<div class="mt-3 flex flex-wrap items-center justify-center gap-2">
	{#if next}
		<a href="{resolve('/devices/[id]', { id: next.id })}#ssh" class={buttonClass('primary', 'sm')}>
			Set up access to {next.name}
		</a>
	{/if}
	<a href={resolve('/devices')} class={buttonClass(next ? 'ghost' : 'secondary', 'sm')}>
		See the devices
	</a>
</div>
