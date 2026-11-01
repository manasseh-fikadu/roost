<!-- Where admins add a check, or change one. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { addCheck, putCheck } from '#lib/api/client.js';
	import type { Check, CheckInput, CheckKind } from '#lib/api/types.js';
	import { inputOf, kindOptions } from '#lib/checks.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Switch from '#lib/ui/Switch.svelte';

	interface Props {
		/** The check to change, or null to add one. */
		check: Check | null;
		open?: boolean;
		/** The groups there are already, to pick from. */
		groups: string[];
	}

	let { check, open = $bindable(false), groups }: Props = $props();

	const id = $props.id();

	const blank: CheckInput = {
		name: '',
		kind: 'http',
		target: '',
		group: 'Custom',
		intervalSec: 60,
		timeoutMs: 5000
	};

	const targets: Record<Exclude<CheckKind, 'ping'>, { label: string; placeholder: string }> = {
		http: { label: 'URL', placeholder: 'https://example.com/health' },
		tcp: { label: 'Host and port', placeholder: '127.0.0.1:5432' },
		dns: { label: 'Host name', placeholder: 'example.com' }
	};

	// The form starts from the check each time the dialog opens, and stays
	// put while it's open, even as new snapshots come, and while it fades
	// out.
	let opened: CheckInput | null = null;
	const initial = $derived.by(() => {
		if (open) {
			opened = untrack(() => (check ? inputOf(check) : { ...blank }));
		}
		return opened;
	});
	let name = $derived(initial?.name ?? '');
	let kind = $derived((initial?.kind ?? 'http') as Exclude<CheckKind, 'ping'>);
	let target = $derived(initial?.target ?? '');
	let group = $derived(initial?.group ?? '');
	let intervalSec: number | undefined = $derived(initial?.intervalSec);
	let timeoutMs: number | undefined = $derived(initial?.timeoutMs);
	let expectStatus: number | undefined = $derived(initial?.expectStatus);
	let insecure = $derived(initial?.insecure ?? false);
	let saving = $state(false);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		const http = kind === 'http';
		const input: CheckInput = {
			name: name.trim(),
			kind,
			target: target.trim(),
			group: group.trim(),
			intervalSec,
			timeoutMs,
			expectStatus: http ? expectStatus || undefined : undefined,
			insecure: http && insecure,
			paused: initial?.paused
		};
		saving = true;
		try {
			if (check) {
				await putCheck(check.id, input);
			} else {
				await addCheck(input);
			}
			open = false;
		} catch {
			// The client showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<Dialog
	bind:open
	title={check ? 'Edit check' : 'New check'}
	description="Checks run from the hub. Use 127.0.0.1 for the hub's own machine, or a tailnet name or address for another."
>
	<form id="{id}-form" class="grid gap-4 sm:grid-cols-2" onsubmit={save}>
		<div class="sm:col-span-2">
			<Field label="Name">
				<Input bind:value={name} placeholder="Postgres" maxlength={80} required />
			</Field>
		</div>
		<Field label="Type">
			<Select bind:value={kind}>
				{#each kindOptions as k (k.value)}
					<option value={k.value}>{k.label}</option>
				{/each}
			</Select>
		</Field>
		<Field label="Group">
			<Input bind:value={group} list="{id}-groups" placeholder="Custom" maxlength={40} />
			<datalist id="{id}-groups">
				{#each groups as g (g)}
					<option value={g}></option>
				{/each}
			</datalist>
		</Field>
		<div class="sm:col-span-2">
			<Field
				label={targets[kind].label}
				hint={kind === 'dns' ? 'The hub looks the name up with its own resolver.' : undefined}
			>
				<Input
					bind:value={target}
					placeholder={targets[kind].placeholder}
					class="font-mono text-[13px]"
					autocomplete="off"
					spellcheck={false}
					required
				/>
			</Field>
		</div>
		<Field label="Every (seconds)" hint="From 10 seconds to a day.">
			<Input type="number" bind:value={intervalSec} min={10} max={86400} placeholder="60" />
		</Field>
		<Field label="Timeout (ms)" hint="Shorter than the interval.">
			<Input
				type="number"
				bind:value={timeoutMs}
				min={200}
				max={60000}
				step={100}
				placeholder="5000"
			/>
		</Field>
		{#if kind === 'http'}
			<Field label="Expected status" hint="Leave it empty to accept any status below 400.">
				<Input
					type="number"
					bind:value={expectStatus}
					min={100}
					max={599}
					placeholder="Any below 400"
				/>
			</Field>
			<div
				class="flex items-center justify-between gap-4 self-start rounded-lg border border-line bg-canvas px-3 py-2.5 sm:mt-6"
			>
				<label for="{id}-insecure">
					<span class="block text-[13px] font-medium text-fg">Skip TLS check</span>
					<span class="block text-[12px] text-fg-subtle">For a certificate of its own</span>
				</label>
				<Switch id="{id}-insecure" bind:checked={insecure} />
			</div>
		{/if}
	</form>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button variant="primary" type="submit" form="{id}-form" loading={saving}>
			{check ? 'Save' : 'Add check'}
		</Button>
	{/snippet}
</Dialog>
