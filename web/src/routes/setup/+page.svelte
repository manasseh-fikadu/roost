<!--
	The first-run setup: whether Roost sees what it needs to, what it's called
	and who the admins are, and the optional rest. Admins see it on their first
	visit, every step can be skipped, and Settings opens it again.
-->
<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { changeSettings, getSettings } from '#lib/api/client.js';
	import type { SettingsView } from '#lib/api/types.js';
	import { live } from '#lib/live.svelte.js';
	import { markSetupSent, nextStep, setupSteps, stepOf, type SetupStep } from '#lib/setup.js';
	import ChecksStep from '#lib/setup/ChecksStep.svelte';
	import ConnectStep from '#lib/setup/ConnectStep.svelte';
	import NameStep from '#lib/setup/NameStep.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';

	const step = $derived(stepOf(page.url.searchParams.get('step')));
	const index = $derived(setupSteps.findIndex((s) => s.value === step));

	function go(to: SetupStep) {
		const url = new URL(page.url.href);
		url.searchParams.set('step', to);
		goto(url, { reset: false });
	}

	let view = $state.raw<SettingsView | null>(null);
	let name = $state('');
	let busy = $state(false);

	// Roost's name and how it joins the tailnet, once.
	$effect(() => {
		let current = true;
		getSettings()
			.then((v) => {
				if (current) {
					view = v;
					name = v.settings.general.name;
				}
			})
			.catch(() => {
				// The checks work without it, and the name step says what's wrong.
			});
		return () => {
			current = false;
		};
	});

	const nameProblem = $derived(name.trim() === '' ? 'Give Roost a name.' : '');

	/** Saves the name if it changed. It reports whether the setup can go on. */
	async function saveName(): Promise<boolean> {
		const trimmed = name.trim();
		if (!view || trimmed === view.settings.general.name) {
			return true;
		}
		if (nameProblem) {
			return false;
		}
		try {
			view = await changeSettings({ general: { name: trimmed } }, 'Named Roost');
			return true;
		} catch {
			// changeSettings showed the error.
			return false;
		}
	}

	/** Goes on to the next step, saving the name first on the step that has one. */
	async function next() {
		if (step === 'name' && !(await saveName())) {
			return;
		}
		skip();
	}

	/** Goes on to the next step, leaving what's on this one. */
	function skip() {
		const to = nextStep(step);
		if (to) {
			go(to);
		}
	}

	/** Marks the setup done, for good, and goes to the overview. */
	async function leave(success: string) {
		busy = true;
		try {
			await changeSettings({ general: { setupDone: true } }, success);
			// The hub's snapshot may not say so yet.
			markSetupSent();
			await goto(resolve('/'));
		} catch {
			// changeSettings showed the error.
		} finally {
			busy = false;
		}
	}
</script>

<AdminOnly>
	<PageHeader
		title="Set up Roost"
		description="A few checks and choices. Every step can be skipped, and Settings opens this again."
	>
		{#snippet actions()}
			<Button variant="ghost" loading={busy} onclick={() => leave('Setup skipped')}>
				Skip setup
			</Button>
		{/snippet}
	</PageHeader>

	<ol class="mb-6 flex flex-wrap gap-x-6 gap-y-2 text-[13px]" aria-label="Setup steps">
		{#each setupSteps as s, i (s.value)}
			<li aria-current={s.value === step ? 'step' : undefined}>
				<button
					type="button"
					class={[
						'flex items-center gap-2 transition-colors',
						s.value === step ? 'text-fg' : 'text-fg-subtle hover:text-fg-muted'
					]}
					onclick={() => go(s.value)}
				>
					<span
						class={[
							'flex size-5 items-center justify-center rounded-full border font-mono text-[11px]',
							s.value === step
								? 'border-fg bg-fg text-canvas'
								: i < index
									? 'border-line-strong text-fg-muted'
									: 'border-line text-fg-faint'
						]}>{i + 1}</span
					>
					{s.label}
				</button>
			</li>
		{/each}
	</ol>

	<div class="max-w-2xl">
		{#if step === 'checks'}
			<ChecksStep snapshot={live.snapshot} me={page.data.me} mode={view?.tailscale} />
		{:else if step === 'name'}
			<NameStep bind:name problem={nameProblem} onsubmit={next} />
		{:else}
			<ConnectStep snapshot={live.snapshot} />
		{/if}

		<div class="mt-6 flex items-center gap-2">
			{#if index > 0}
				<Button variant="ghost" onclick={() => go(setupSteps[index - 1].value)}>
					{#snippet icon()}<ArrowLeft class="size-3.5" />{/snippet}
					Back
				</Button>
			{/if}
			<span class="ml-auto"></span>
			{#if nextStep(step)}
				{#if step === 'name'}
					<Button variant="ghost" onclick={skip}>Skip this step</Button>
				{/if}
				<Button variant="primary" onclick={next}>
					Continue
					<ArrowRight class="size-3.5" />
				</Button>
			{:else}
				<Button variant="primary" loading={busy} onclick={() => leave('Setup finished')}>
					Finish setup
				</Button>
			{/if}
		</div>
	</div>
	{#snippet fallback()}
		<Card>
			<EmptyState title="Only admins can set up Roost">
				Ask an admin to open this page, or to add you as one.
			</EmptyState>
		</Card>
	{/snippet}
</AdminOnly>
