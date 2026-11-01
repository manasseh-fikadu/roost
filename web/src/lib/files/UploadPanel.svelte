<!--
	The uploads in progress and just finished, in a panel at the corner of the
	screen. The queue (uploads.svelte.ts) outlives the page, so the panel
	shows on any page that renders it.
-->
<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import ChevronUp from '@lucide/svelte/icons/chevron-up';
	import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
	import Upload from '@lucide/svelte/icons/upload';
	import X from '@lucide/svelte/icons/x';
	import { bytes, plural, rate } from '#lib/format.js';
	import IconButton from '#lib/ui/IconButton.svelte';
	import FileIcon from './FileIcon.svelte';
	import { uploads, type UploadStatus } from './uploads.svelte.js';
	import { eta, uploadSummary } from './uploadpanel.js';

	let collapsed = $state(false);

	const tasks = $derived(uploads.tasks);
	const s = $derived(uploadSummary(tasks));

	/** What's moving first, then failures, then the rest. */
	const order: Record<UploadStatus, number> = {
		uploading: 0,
		error: 1,
		queued: 2,
		canceled: 3,
		done: 4
	};
	const sorted = $derived(
		[...tasks].sort((a, b) => order[a.status] - order[b.status] || a.id - b.id).slice(0, 200)
	);
</script>

{#if tasks.length}
	<div
		class="fixed inset-x-2 bottom-[max(0.5rem,env(safe-area-inset-bottom))] z-40 overflow-hidden rounded-xl border border-line-strong bg-surface shadow-popover sm:inset-x-auto sm:right-4 sm:bottom-4 sm:w-[380px]"
	>
		<div class="flex items-center gap-2.5 px-3 py-2.5">
			<span
				class={[
					'flex size-7 shrink-0 items-center justify-center rounded-md border',
					s.failed && !s.active ? 'border-red/40 text-red-fg' : 'border-line-strong text-fg-muted'
				]}
			>
				{#if s.active}
					<Upload class="size-3.5" />
				{:else if s.failed}
					<X class="size-3.5" />
				{:else}
					<Check class="size-3.5 text-green-fg" />
				{/if}
			</span>
			<div class="min-w-0 flex-1">
				<div class="truncate text-[13px] font-medium text-fg">
					{#if s.active}
						Uploading {plural(s.active, 'file')}
					{:else if s.failed}
						{plural(s.failed, 'upload')} failed
					{:else}
						{plural(s.done, 'upload')} complete
					{/if}
				</div>
				<div class="truncate num text-[11px] text-fg-subtle">
					{#if s.active}
						{Math.floor(s.percent)}% of {bytes(s.total)}
						{#if s.speed > 0}
							· {rate(s.speed)} · {eta((s.total - s.loaded) / s.speed)}
						{/if}
					{:else}
						{bytes(s.loaded)}
						{#if s.failed && s.done}· {s.done} done{/if}
					{/if}
				</div>
			</div>
			{#if s.active}
				<button
					type="button"
					onclick={() => uploads.cancelAll()}
					class="h-7 rounded-md px-2 text-[12px] text-fg-subtle hover:bg-hover hover:text-fg"
				>
					Cancel all
				</button>
			{/if}
			<IconButton
				label={collapsed ? 'Expand' : 'Collapse'}
				size="sm"
				onclick={() => (collapsed = !collapsed)}
			>
				{#if collapsed}<ChevronUp class="size-4" />{:else}<ChevronDown class="size-4" />{/if}
			</IconButton>
			{#if !s.active}
				<IconButton label="Close" size="sm" onclick={() => uploads.clearFinished()}>
					<X class="size-4" />
				</IconButton>
			{/if}
		</div>
		<div class="h-0.5 bg-line">
			<div
				class={[
					'h-full transition-[width] duration-300',
					s.failed && !s.active ? 'bg-red-fg' : s.active ? 'bg-blue-fg' : 'bg-green-fg'
				]}
				style:width="{s.percent}%"
			></div>
		</div>
		{#if !collapsed}
			<ul class="max-h-72 divide-y divide-line overflow-y-auto">
				{#each sorted as t (t.id)}
					{@const name = t.name.split('/').pop() ?? t.name}
					{@const folder = t.name.includes('/') ? t.name.slice(0, t.name.lastIndexOf('/')) : null}
					<li class="relative flex items-center gap-2.5 px-3 py-2">
						<FileIcon entry={{ name, type: 'file' }} />
						<div class="min-w-0 flex-1">
							<div class="flex items-baseline gap-1.5">
								<span class="truncate text-[13px] text-fg" title={t.name}>{name}</span>
								{#if folder}
									<span class="truncate font-mono text-[10.5px] text-fg-faint">{folder}/</span>
								{/if}
							</div>
							<div
								class={[
									'truncate num text-[11px]',
									t.status === 'error' ? 'text-red-fg' : 'text-fg-subtle'
								]}
								title={t.error ?? undefined}
							>
								{#if t.status === 'uploading'}
									{bytes(t.loaded)} of {bytes(t.size)}
									{#if t.speed > 0}<span class="text-fg-faint">· {rate(t.speed)}</span>{/if}
								{:else if t.status === 'queued'}
									Waiting · {bytes(t.size)}
								{:else if t.status === 'done'}
									{#if t.result && !t.result.endsWith(`/${t.name}`)}
										Saved as {t.result.split('/').pop()}
									{:else}
										{bytes(t.size)}
									{/if}
								{:else if t.status === 'error'}
									{t.error}
								{:else}
									Canceled
								{/if}
							</div>
							{#if t.status === 'uploading'}
								<div class="mt-1 h-0.5 overflow-hidden rounded-full bg-line-strong">
									<div
										class="h-full rounded-full bg-blue-fg transition-[width] duration-200"
										style:width="{t.size ? (t.loaded / t.size) * 100 : 0}%"
									></div>
								</div>
							{/if}
						</div>
						{#if t.status === 'done'}
							<Check class="size-3.5 shrink-0 text-green-fg" />
						{:else if t.status === 'uploading' || t.status === 'queued'}
							<IconButton label="Cancel upload" size="sm" onclick={() => uploads.cancel(t.id)}>
								<X class="size-3.5" />
							</IconButton>
						{:else}
							<IconButton label="Retry" size="sm" onclick={() => uploads.retry(t.id)}>
								<RotateCcw class="size-3.5" />
							</IconButton>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</div>
{/if}
