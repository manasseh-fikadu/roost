<!--
	Shows its children only to admins, and `fallback` (if given) to everyone
	else. This is where the UI hides what only admins can do; the server
	enforces it either way.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';

	interface Props {
		/** What admins see. */
		children: Snippet;
		/** What everyone else sees instead. Nothing if it's not given. */
		fallback?: Snippet;
	}

	let { children, fallback }: Props = $props();
</script>

{#if isAdmin(page.data.me)}
	{@render children()}
{:else}
	{@render fallback?.()}
{/if}
