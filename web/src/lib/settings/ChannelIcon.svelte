<!--
	The logo of the service a notification channel sends to, or for email and
	generic webhooks, which have none, an icon. It takes the color of the text
	around it.
-->
<script lang="ts">
	import Mail from '@lucide/svelte/icons/mail';
	import Webhook from '@lucide/svelte/icons/webhook';
	import type { ClassValue } from 'svelte/elements';
	import type { ChannelKind } from '#lib/api/types.js';
	import { brandPaths } from './brandPaths.js';

	interface Props {
		kind: ChannelKind;
		class?: ClassValue;
	}

	let { kind, class: className }: Props = $props();

	const path = $derived(brandPaths[kind]);
</script>

{#if path}
	<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" class={className}>
		<path d={path} />
	</svg>
{:else if kind === 'email'}
	<Mail class={className} strokeWidth={1.75} />
{:else}
	<Webhook class={className} strokeWidth={1.75} />
{/if}
