<!--
	A container's logs, live. Lines stream in over server-sent events into a
	buffer of the newest 10,000, and only the rows on screen are in the page.
	With Wrap off every row is one line high, so the rows to draw come from the
	scroll position. With Wrap on, rows have no known height, so the newest
	2,000 are drawn and a button reaches for more. Scrolling up stops
	following and freezes the view, so lines arriving don't move what is being
	read.
-->
<script lang="ts">
	import { tick, untrack } from 'svelte';
	import ArrowDown from '@lucide/svelte/icons/arrow-down';
	import Download from '@lucide/svelte/icons/download';
	import Eraser from '@lucide/svelte/icons/eraser';
	import Regex from '@lucide/svelte/icons/regex';
	import Search from '@lucide/svelte/icons/search';
	import { Persisted } from '#lib/files/persisted.svelte.js';
	import type { Status } from '#lib/status.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import { dockerLogDownloadUrl } from './api.js';
	import LogLine from './LogLine.svelte';
	import LogToggle from './LogToggle.svelte';
	import {
		compileSearch,
		defaultTail,
		filterLines,
		lineCount,
		rowHeight,
		scrollRange,
		sinceOptions,
		tailOptions,
		tailRange,
		wrapWindow,
		type LogEntry
	} from './logs.js';
	import { LogStream, type StreamQuery, type StreamStatus } from './logstream.svelte.js';

	interface Props {
		deviceId: string;
		/** The container's name. */
		container: string;
		running: boolean;
		/**
		 * When the container last started, as the hub says it. A restart doesn't
		 * change `running`, so a new time is how the log knows to pick up again.
		 */
		startedAt?: string;
		/** Whether the container has a terminal, which merges its standard output and error. */
		tty: boolean;
	}

	let { deviceId, container, running, startedAt, tty }: Props = $props();

	/** How many more lines a click on "earlier lines" draws when lines wrap. */
	const wrapStep = 1_000;
	/** How far from the end a scroll may stop and still be following. */
	const followSlack = 40;
	/** The space above and below the lines, in pixels. */
	const gap = 8;

	const stream = new LogStream();
	const timestamps = new Persisted('docker.logs.timestamps', false);
	const wrap = new Persisted('docker.logs.wrap', true);

	let tailChoice = $state(String(defaultTail));
	let since = $state('');
	let stdout = $state(true);
	let stderr = $state(true);
	let text = $state('');
	let regex = $state(false);
	/** What was typed, once typing paused. */
	let applied = $state('');
	let follow = $state(true);
	/** The lines as they were when following stopped, or null while following. */
	let frozen = $state.raw<LogEntry[] | null>(null);
	/** How many lines had arrived when following stopped. */
	let frozenAt = $state(0);
	let wrapCap = $state(wrapWindow);

	let scroller = $state<HTMLDivElement>();
	let viewport = $state(0);
	let boxWidth = $state(0);
	let scrollTop = $state(0);
	/** Where the last scroll that this component made left the box. */
	let lastAuto = -1;

	// A terminal sends everything as output, so there are no streams to choose from.
	const wantStdout = $derived(tty || stdout);
	const wantStderr = $derived(tty || stderr);

	// A new stream for another container, or for other choices.
	$effect(() => {
		const query: StreamQuery = {
			tail: Number(tailChoice),
			since,
			stdout: wantStdout,
			stderr: wantStderr
		};
		const device = deviceId;
		const name = container;
		follow = true;
		frozen = null;
		wrapCap = wrapWindow;
		return stream.connect(
			device,
			name,
			query,
			untrack(() => running)
		);
	});

	// A container that starts again, or is restarted, picks its stream up where
	// it ended.
	$effect(() => {
		void startedAt;
		if (running) {
			untrack(() => {
				if (stream.status === 'ended') {
					stream.resume(true);
				}
			});
		}
	});

	// Searching waits for a pause in typing.
	$effect(() => {
		const typed = text;
		const timer = setTimeout(() => (applied = typed), typed === '' ? 0 : 150);
		return () => clearTimeout(timer);
	});

	const search = $derived(compileSearch(applied, regex));
	const searching = $derived(search.test !== null);
	const source = $derived(frozen ?? stream.lines);
	const rows = $derived(filterLines(source, search));
	const lastN = $derived(rows.length > 0 ? rows[rows.length - 1].n : 0);

	const range = $derived(
		follow ? tailRange(viewport, rows.length) : scrollRange(scrollTop, viewport, rows.length)
	);
	const start = $derived(range.start);
	const end = $derived(range.end);
	const hiddenEarlier = $derived(wrap.value ? Math.max(0, rows.length - wrapCap) : 0);
	const drawn = $derived(
		wrap.value ? rows.slice(rows.length - Math.min(rows.length, wrapCap)) : rows.slice(start, end)
	);
	/** Lines that arrived while the view was frozen. */
	const fresh = $derived(frozen ? stream.received - frozenAt : 0);

	function pause() {
		if (follow) {
			follow = false;
			frozen = stream.lines;
			frozenAt = stream.received;
		}
	}

	function resumeFollow() {
		follow = true;
		frozen = null;
	}

	// Follow the end: after the lines change, and when a change of size moves it.
	$effect(() => {
		void [lastN, rows.length, wrap.value, timestamps.value, viewport, boxWidth, follow];
		if (follow && scroller) {
			scroller.scrollTop = scroller.scrollHeight;
			lastAuto = scroller.scrollTop;
		}
	});

	function onscroll() {
		const el = scroller;
		if (!el) {
			return;
		}
		scrollTop = el.scrollTop;
		if (follow && Math.abs(el.scrollTop - lastAuto) < 2) {
			// This is the scroll to the end that the effect above made.
			return;
		}
		const away = el.scrollHeight - el.scrollTop - el.clientHeight;
		if (follow && away > followSlack) {
			pause();
		} else if (!follow && away < 2) {
			// Back at the end: follow again.
			resumeFollow();
		}
	}

	async function showEarlier() {
		const el = scroller;
		if (!el) {
			return;
		}
		// New rows go above the ones there are, so keep the distance from the end.
		const fromEnd = el.scrollHeight - el.scrollTop;
		wrapCap += wrapStep;
		await tick();
		el.scrollTop = el.scrollHeight - fromEnd;
	}

	function clear() {
		stream.clear();
		wrapCap = wrapWindow;
		resumeFollow();
	}

	const downloadHref = $derived(
		dockerLogDownloadUrl(deviceId, container, {
			tail: 'all',
			since: since || undefined,
			stdout: wantStdout,
			stderr: wantStderr,
			timestamps: timestamps.value
		})
	);

	const dot: Record<StreamStatus, Status> = {
		connecting: 'attention',
		loading: 'attention',
		live: 'online',
		reconnecting: 'attention',
		ended: 'unknown',
		failed: 'offline'
	};

	const statusText = $derived.by(() => {
		switch (stream.status) {
			case 'connecting':
				return 'Connecting…';
			case 'loading':
				return 'Loading…';
			case 'live':
				return follow ? 'Live' : 'Live · paused';
			case 'ended':
				return `Ended: ${stream.message ?? 'the log ended'}`;
			default:
				return stream.message ?? 'The log failed';
		}
	});

	const emptyText = $derived.by(() => {
		if (searching && source.length > 0) {
			return `No lines match “${applied.trim()}”.`;
		}
		switch (stream.status) {
			case 'connecting':
			case 'loading':
				return 'Loading logs…';
			case 'reconnecting':
			case 'failed':
				return stream.message ?? 'The log failed';
			default:
				return running ? 'No log output yet.' : 'This container has no log output.';
		}
	});

	const selectClass = 'h-8! w-auto! text-[12.5px]';
</script>

<Card>
	<div class="flex flex-wrap items-start gap-2 border-b border-line p-3">
		<div class="min-w-0 flex-1 basis-56">
			<div class="relative">
				<Search
					class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-fg-faint"
				/>
				<Input
					bind:value={text}
					placeholder="Search the log…"
					aria-label="Search the log"
					autocomplete="off"
					spellcheck={false}
					class="h-8! pl-8 font-mono text-[12.5px]"
				/>
			</div>
			{#if search.error}
				<p class="mt-1 text-[12px] text-red-fg" role="status">{search.error}</p>
			{/if}
		</div>
		<LogToggle
			bind:pressed={regex}
			label="Regular expression"
			title="Search with a regular expression"
		>
			<Regex class="size-3.5" />
		</LogToggle>
		<LogToggle
			bind:pressed={() => follow, (on) => (on ? resumeFollow() : pause())}
			title="Scroll to new lines as they arrive"
		>
			Follow
		</LogToggle>
		<a
			href={downloadHref}
			download
			title="Download the whole log as text, with the streams and time chosen here"
			class={buttonClass('secondary', 'sm')}
		>
			<Download class="size-3.5" />
			<span class="hidden sm:inline">Download</span>
		</a>
		<Button variant="ghost" onclick={clear} title="Clear the view. New lines keep arriving.">
			{#snippet icon()}<Eraser class="size-3.5" />{/snippet}
			<span class="hidden sm:inline">Clear</span>
		</Button>
	</div>
	<div class="flex flex-wrap items-center gap-2 border-b border-line bg-canvas/40 px-3 py-2">
		{#if tty}
			<span class="text-[12.5px] text-fg-subtle">
				This container has a terminal, so its output and errors come together.
			</span>
		{:else}
			<LogToggle
				bind:pressed={stdout}
				disabled={stdout && !stderr}
				title={stdout && !stderr ? 'One of the two has to be on' : undefined}
			>
				<span class="size-1.5 rounded-full bg-fg-muted"></span>
				stdout
			</LogToggle>
			<LogToggle
				bind:pressed={stderr}
				disabled={stderr && !stdout}
				title={stderr && !stdout ? 'One of the two has to be on' : undefined}
			>
				<span class="size-1.5 rounded-full bg-red-fg"></span>
				stderr
			</LogToggle>
		{/if}
		<span class="mx-1 hidden h-4 w-px bg-line-strong sm:block"></span>
		<LogToggle bind:pressed={timestamps.value}>Timestamps</LogToggle>
		<LogToggle bind:pressed={wrap.value}>Wrap</LogToggle>
		<div class="ml-auto flex items-center gap-2">
			<Select bind:value={tailChoice} aria-label="Lines to start with" class={selectClass}>
				{#each tailOptions as n (n)}
					<option value={String(n)}>Last {n.toLocaleString()} lines</option>
				{/each}
			</Select>
			<Select bind:value={since} aria-label="Since" class={selectClass}>
				{#each sinceOptions as o (o.value)}
					<option value={o.value}>{o.label}</option>
				{/each}
			</Select>
		</div>
	</div>

	<div class="relative">
		<!-- A box that scrolls has to take the keyboard, and a log is read with it. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			bind:this={scroller}
			bind:clientHeight={viewport}
			bind:clientWidth={boxWidth}
			{onscroll}
			role="log"
			aria-label="Logs of {container}"
			aria-live="off"
			tabindex="0"
			class="h-[65vh] min-h-80 overflow-auto bg-canvas font-mono text-[12px] [overflow-anchor:none]"
		>
			{#if rows.length === 0}
				<div
					class="grid h-full place-items-center px-6 text-center font-sans text-[13px] text-fg-subtle"
				>
					{emptyText}
				</div>
			{:else if wrap.value}
				<div class="py-2">
					{#if hiddenEarlier > 0}
						<div class="px-3 pb-2 font-sans text-[12.5px] text-fg-subtle">
							<button
								type="button"
								onclick={showEarlier}
								class="text-fg underline-offset-2 hover:underline"
							>
								Show {Math.min(wrapStep, hiddenEarlier).toLocaleString()} earlier lines
							</button>
							<span
								>({hiddenEarlier.toLocaleString()} not drawn. Turn Wrap off to scroll through all.)</span
							>
						</div>
					{/if}
					{#each drawn as entry (entry.n)}
						<LogLine {entry} wrap={true} timestamps={timestamps.value} {search} />
					{/each}
				</div>
			{:else}
				<div
					class="w-max min-w-full"
					style:padding-top="{gap + start * rowHeight}px"
					style:padding-bottom="{gap + Math.max(0, rows.length - end) * rowHeight}px"
				>
					{#each drawn as entry (entry.n)}
						<LogLine {entry} wrap={false} timestamps={timestamps.value} {search} />
					{/each}
				</div>
			{/if}
		</div>
		{#if !follow}
			<button
				type="button"
				onclick={resumeFollow}
				class="absolute bottom-3 left-1/2 inline-flex h-8 -translate-x-1/2 items-center gap-1.5 rounded-full border border-line-strong bg-surface/95 px-3 text-[12.5px] font-medium text-fg shadow-popover backdrop-blur hover:bg-hover"
			>
				<ArrowDown class="size-3.5" />
				{fresh > 0 ? `Jump to latest · ${fresh.toLocaleString()} new` : 'Jump to latest'}
			</button>
		{/if}
	</div>

	<div
		class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-line px-3 py-2 text-[12px] text-fg-subtle"
	>
		<span class="flex min-w-0 items-center gap-2">
			<StatusDot status={dot[stream.status]} pulse={stream.status === 'live' && follow} />
			<span class={['truncate', stream.status === 'failed' && 'text-red-fg']} role="status">
				{statusText}
			</span>
			{#if stream.status === 'ended' || stream.status === 'failed'}
				<Button size="xs" variant="ghost" onclick={() => stream.resume(running)}>Reconnect</Button>
			{/if}
		</span>
		<span class="num">{lineCount(rows.length, source.length, searching)}</span>
	</div>
</Card>
