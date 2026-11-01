<script lang="ts">
	import Inbox from '@lucide/svelte/icons/inbox';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Plus from '@lucide/svelte/icons/plus';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { mutate } from '#lib/api/client.js';
	import type { CheckResult } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import HealthRing from '#lib/charts/HealthRing.svelte';
	import Sparkline from '#lib/charts/Sparkline.svelte';
	import UptimeBars from '#lib/charts/UptimeBars.svelte';
	import type { Point } from '#lib/charts/path.js';
	import type { Status, Tone } from '#lib/status.js';
	import { toast } from '#lib/toast.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Field from '#lib/ui/Field.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Kbd from '#lib/ui/Kbd.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import Meter from '#lib/ui/Meter.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';
	import Textarea from '#lib/ui/Textarea.svelte';
	import Tooltip from '#lib/ui/Tooltip.svelte';
	import { buttonClass } from '#lib/ui/button.js';

	const statuses: Status[] = ['online', 'attention', 'offline', 'unknown'];
	const tones: Tone[] = ['green', 'amber', 'red', 'blue', 'purple', 'gray'];

	// An hour of made-up CPU and memory, a point a minute, with a gap where
	// the agent was quiet but for one reading.
	const now = Math.floor(Date.now() / 60_000) * 60_000;
	const minutes = Array.from({ length: 60 }, (_, i) => i);
	const quiet = (i: number) => i >= 37 && i < 44 && i !== 40;
	const cpu: Point[] = minutes.map((i) => ({
		ts: now - (59 - i) * 60_000,
		v: quiet(i) ? null : 22 + 14 * Math.sin(i / 6) + 6 * Math.sin(i / 1.7)
	}));
	const memory: Point[] = minutes.map((i) => ({
		ts: now - (59 - i) * 60_000,
		v: quiet(i) ? null : 48 + 4 * Math.sin(i / 11)
	}));

	// Half an hour of made-up results of a check, a minute apart, with two
	// that failed.
	const results: CheckResult[] = Array.from({ length: 30 }, (_, i) => {
		const ok = i !== 12 && i !== 25;
		return {
			at: new Date(now - (29 - i) * 60_000).toISOString(),
			ok,
			latencyMs: ok ? 18 + (i % 5) * 3 : undefined,
			detail: ok ? 'HTTP 200 OK' : 'dial tcp: connect: connection refused'
		};
	});

	let name = $state('');
	let kind = $state('');
	let notes = $state('');
	let alerts = $state(true);
	let range = $state('24h');
	let dialogOpen = $state(false);
	let tab = $state('cpu');
	let saving = $state(false);
	let picked: number | null = $state(null);

	async function failingWrite() {
		saving = true;
		try {
			await mutate('POST', 'design/nothing-here');
		} catch {
			// mutate showed the error as a toast.
		} finally {
			saving = false;
		}
	}
</script>

<PageHeader
	title="Design system"
	description="Every component, for working on them. Only make dev serves this page."
>
	{#snippet actions()}
		<Button variant="primary" onclick={() => toast('Saved')}>
			{#snippet icon()}<Plus class="size-3.5" />{/snippet}
			Primary action
		</Button>
	{/snippet}
</PageHeader>

<div class="grid gap-4 xl:grid-cols-2">
	<Card
		title="Buttons"
		description="Four variants in three sizes, a loading state, and icon buttons."
	>
		<div class="space-y-3 p-4">
			{#each ['md', 'sm', 'xs'] as const as size (size)}
				<div class="flex flex-wrap items-center gap-2">
					<Button variant="primary" {size}>Primary</Button>
					<Button {size}>Secondary</Button>
					<Button variant="ghost" {size}>Ghost</Button>
					<Button variant="danger" {size}>Danger</Button>
				</div>
			{/each}
			<div class="flex flex-wrap items-center gap-2">
				<Button loading>Saving</Button>
				<Button disabled>Disabled</Button>
				<a href="/" class={buttonClass('secondary', 'sm')}>A link</a>
				<IconButton label="Refresh"><RefreshCw class="size-4" /></IconButton>
				<IconButton label="Edit" size="sm"><Pencil class="size-3.5" /></IconButton>
			</div>
		</div>
	</Card>

	<Card title="Status" description="Dots, pills and badges.">
		<div class="space-y-4 p-4">
			<div class="flex flex-wrap items-center gap-4">
				{#each statuses as s (s)}
					<span class="inline-flex items-center gap-2 text-[13px]">
						<StatusDot status={s} pulse />
						{s}
					</span>
				{/each}
			</div>
			<div class="flex flex-wrap gap-2">
				{#each statuses as s (s)}
					<StatusPill status={s} />
				{/each}
				<StatusPill status="unknown" label="STOPPED" />
			</div>
			<div class="flex flex-wrap gap-2">
				{#each tones as tone (tone)}
					<Badge {tone}>{tone}</Badge>
				{/each}
			</div>
		</div>
	</Card>

	<Card title="Inputs">
		<div class="space-y-4 p-4">
			<Field label="Display name" hint="Shown in place of the hostname.">
				<Input bind:value={name} placeholder="MacBook Pro" />
			</Field>
			<Field label="Type">
				<Select bind:value={kind}>
					<option value="">Detect automatically</option>
					<option value="laptop">Laptop</option>
					<option value="server">Server</option>
				</Select>
			</Field>
			<Field label="Notes">
				<Textarea bind:value={notes} placeholder="Where it lives, what runs on it…" />
			</Field>
			<div class="flex flex-wrap items-center gap-4">
				<Switch bind:checked={alerts} label="Alert when offline" />
				<Segmented
					bind:value={range}
					label="Time range"
					options={['1h', '6h', '24h', '7d'].map((r) => ({ value: r, label: r }))}
				/>
			</div>
			<CodeLine command="bield admin add-viewer bob@example.com" />
			<p class="text-[13px] text-fg-subtle">
				Keys: <Kbd>⌘</Kbd>
				<Kbd>K</Kbd> opens the palette, <Kbd>esc</Kbd> closes it.
			</p>
		</div>
	</Card>

	<Card title="Overlays" description="Dialog, menu, tooltip and toasts.">
		<div class="flex flex-wrap items-center gap-2 p-4">
			<Dialog
				bind:open={dialogOpen}
				title="Edit device"
				description="Change how this device shows up in Bield."
			>
				{#snippet trigger({ props })}
					<Button {...props}>Open dialog</Button>
				{/snippet}
				<Field label="Display name">
					<Input bind:value={name} placeholder="MacBook Pro" />
				</Field>
				{#snippet footer()}
					<Button variant="ghost" onclick={() => (dialogOpen = false)}>Cancel</Button>
					<Button
						variant="primary"
						onclick={() => {
							dialogOpen = false;
							toast('Device updated');
						}}>Save</Button
					>
				{/snippet}
			</Dialog>
			<Menu align="start">
				{#snippet trigger({ props })}
					<Button {...props}>Open menu</Button>
				{/snippet}
				<MenuItem icon={Pencil} onSelect={() => toast('Edit')}>Edit</MenuItem>
				<MenuItem icon={RefreshCw} onSelect={() => toast('Restarted')}>Restart</MenuItem>
				<MenuSeparator />
				<MenuItem icon={Trash2} danger onSelect={() => toast('Removed')}>Remove</MenuItem>
			</Menu>
			<Tooltip content="A tooltip">
				{#snippet trigger({ props })}
					<Button {...props} variant="ghost">Hover me</Button>
				{/snippet}
			</Tooltip>
			<Button onclick={() => toast('Check saved')}>Toast</Button>
			<Button loading={saving} onclick={failingWrite}>Failing request</Button>
		</div>
	</Card>

	<Card title="Charts" class="xl:col-span-2">
		<div class="p-4">
			<Tabs
				bind:value={tab}
				tabs={[
					{ value: 'cpu', label: 'CPU and memory' },
					{ value: 'small', label: 'Sparklines, bars and rings', count: 4 }
				]}
			>
				<TabPanel value="cpu">
					<AreaChart
						label="CPU and memory over the last hour"
						series={[
							{ key: 'cpu', label: 'CPU', color: 'var(--color-blue-fg)', points: cpu },
							{ key: 'mem', label: 'Memory', color: 'var(--color-purple-fg)', points: memory }
						]}
						yMax={100}
						format={(v) => `${v.toFixed(0)}%`}
						onSelect={(ts) => (picked = ts)}
						fill={false}
					/>
					<p class="mt-2 text-[12px] text-fg-subtle">
						{picked === null
							? 'Click the chart, or focus it and press Enter, to pick a time.'
							: `Picked ${new Date(picked).toLocaleTimeString()}.`}
					</p>
				</TabPanel>
				<TabPanel value="small">
					<div class="grid gap-6 sm:grid-cols-[1fr_auto]">
						<div class="space-y-4">
							<Sparkline values={cpu.map((p) => p.v)} />
							<Sparkline
								values={memory.map((p) => p.v)}
								color="var(--color-purple-fg)"
								fill={false}
							/>
							<UptimeBars results={results.slice(0, 20)} />
							<UptimeBars {results} class="w-56" />
						</div>
						<div class="flex gap-3">
							<HealthRing score={100} />
							<HealthRing score={82} />
							<HealthRing score={45} />
							<HealthRing score={null} />
						</div>
					</div>
				</TabPanel>
			</Tabs>
		</div>
	</Card>

	<Card title="Facts and meters" description="Rows in a card's list, and how full something is.">
		<dl>
			<DataRow label="Tailnet IP" copy="100.64.0.2">100.64.0.2</DataRow>
			<DataRow label="Path">direct</DataRow>
		</dl>
		<div class="space-y-3 p-4">
			{#each [42, 86, 97, null] as value, i (i)}
				<Meter {value} warn={85} crit={95} />
			{/each}
		</div>
	</Card>

	<Card title="Loading and empty">
		<div class="space-y-2 p-4">
			<Skeleton class="h-6 w-40" />
			<Skeleton class="h-4 w-72" />
			<Skeleton class="h-24 rounded-xl" />
		</div>
	</Card>

	<Card>
		<EmptyState title="No containers" icon={Inbox}>
			Containers on this machine show up here when Docker is running.
		</EmptyState>
	</Card>
</div>
