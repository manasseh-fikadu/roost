<script lang="ts">
	import Compass from '@lucide/svelte/icons/compass';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import EmptyState from '#lib/ui/EmptyState.svelte';
</script>

<EmptyState title={page.status === 404 ? 'No page here' : 'Something went wrong'} icon={Compass}>
	{#if page.status !== 404 && page.error?.message}
		<span class="block">{page.error.message}</span>
	{/if}
	<a href={resolve('/')} class="text-fg underline underline-offset-4">Go to the overview</a>
</EmptyState>
