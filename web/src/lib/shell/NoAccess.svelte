<!--
	What someone sees instead of the app when they can't view Roost, or when
	the hub didn't say who they are.
-->
<script lang="ts">
	import { page } from '$app/state';
	import { addViewerCommand, whyCantView } from '#lib/access.js';
	import Button from '#lib/ui/Button.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import Logo from './Logo.svelte';

	const me = $derived(page.data.me);
	const command = $derived(me ? addViewerCommand(me) : undefined);
</script>

<svelte:head>
	<title>Roost</title>
</svelte:head>

<main class="grid min-h-dvh place-items-center p-6">
	<section class="w-full max-w-md card p-6">
		<div class="flex items-center gap-2.5">
			<span
				class="flex size-7 items-center justify-center rounded-md border border-line-strong bg-raised text-fg"
			>
				<Logo class="size-4" />
			</span>
			<h1 class="text-[16px] font-semibold tracking-[-0.01em]">Roost</h1>
		</div>
		{#if me}
			<p class="mt-4 text-[14px] text-fg-muted">{whyCantView(me)}</p>
			{#if command}
				<p class="mt-4 mb-2 text-[13px] text-fg-subtle">
					They can run this on the machine Roost runs on:
				</p>
				<CodeLine {command} />
			{/if}
		{:else}
			<p class="mt-4 flex items-center gap-2 text-[14px] text-red-fg">
				<StatusDot status="offline" />
				Can't reach the hub: {page.data.hubError}
			</p>
			<Button class="mt-4" onclick={() => location.reload()}>Try again</Button>
		{/if}
	</section>
</main>
