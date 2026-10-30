<!--
	A Markdown document, rendered for reading. It draws a tree from
	parseMarkdown() as elements and never as HTML, so a hostile file can't
	inject markup: raw HTML in the source shows as text. Links open in a new
	tab and only http, https and mailto links are live; a relative link or image
	goes through `resolve`. Images load from https and from the machine, and
	send no referrer.
-->
<script lang="ts">
	import {
		markdownResolver,
		parseMarkdown,
		type Align,
		type Block,
		type Inline,
		type ListItem
	} from './markdown.js';

	interface Props {
		text: string;
		/**
		 * The URL of the file a relative link or image names, on this origin. It answers null for
		 * one that can't be reached.
		 */
		resolve: (relative: string) => string | null;
	}

	let { text, resolve }: Props = $props();

	const blocks = $derived(parseMarkdown(text, markdownResolver(resolve)));

	const headings = [
		'text-[26px] border-b border-line pb-2',
		'text-[21px] border-b border-line pb-1.5',
		'text-[18px]',
		'text-[16px]',
		'text-[14px]',
		'text-[13px] text-fg-muted'
	];
	const alignments: Record<Align, string> = {
		left: 'text-left',
		center: 'text-center',
		right: 'text-right'
	};
</script>

{#snippet spans(nodes: Inline[])}
	{#each nodes as node, i (i)}
		{#if node.type === 'text'}
			{node.text}
		{:else if node.type === 'code'}
			<code class="rounded bg-hover px-1.5 py-0.5 font-mono text-[0.86em] text-fg">{node.text}</code
			>
		{:else if node.type === 'image'}
			{#if node.src}
				<img
					src={node.src}
					alt={node.alt}
					loading="lazy"
					decoding="async"
					referrerpolicy="no-referrer"
					class="inline max-w-full rounded"
				/>
			{:else}
				[{node.alt || 'image'}]
			{/if}
		{:else if node.type === 'link'}
			{#if node.href}
				<a
					href={node.href}
					target="_blank"
					rel="noopener noreferrer"
					class="text-blue-fg underline-offset-2 hover:underline">{@render spans(node.children)}</a
				>
			{:else}
				<span class="text-blue-fg">{@render spans(node.children)}</span>
			{/if}
		{:else if node.type === 'strong'}
			<strong class="font-semibold text-fg">{@render spans(node.children)}</strong>
		{:else if node.type === 'del'}
			<del>{@render spans(node.children)}</del>
		{:else}
			<em>{@render spans(node.children)}</em>
		{/if}
	{/each}
{/snippet}

{#snippet listItems(list: ListItem[])}
	{#each list as item, i (i)}
		<li class={['leading-7', item.checked !== null && '-ml-5 list-none']}>
			{#if item.checked !== null}
				<input
					type="checkbox"
					checked={item.checked}
					disabled
					class="mr-2 align-middle accent-blue"
				/>
			{/if}
			{@render spans(item.inline)}
			{@render blockList(item.blocks)}
		</li>
	{/each}
{/snippet}

{#snippet blockList(nodes: Block[])}
	{#each nodes as block, i (i)}
		{#if block.type === 'paragraph'}
			<p class="my-3 leading-7 text-fg-muted">{@render spans(block.inline)}</p>
		{:else if block.type === 'heading'}
			<svelte:element
				this={`h${block.level}`}
				class={[
					'mt-6 mb-3 font-semibold tracking-[-0.01em] text-fg first:mt-0',
					headings[block.level - 1]
				]}
			>
				{@render spans(block.inline)}
			</svelte:element>
		{:else if block.type === 'code'}
			<pre
				class="my-4 overflow-x-auto rounded-lg border border-line bg-canvas p-3.5 font-mono text-[12.5px] leading-relaxed text-fg"
				data-lang={block.lang || undefined}><code>{block.text}</code></pre>
		{:else if block.type === 'rule'}
			<hr class="my-6 border-line-strong" />
		{:else if block.type === 'quote'}
			<blockquote class="my-4 border-l-2 border-line-heavy pl-4 text-fg-subtle">
				{@render blockList(block.blocks)}
			</blockquote>
		{:else if block.type === 'table'}
			<div class="my-4 overflow-x-auto">
				<table class="text-[13px]">
					<thead>
						<tr>
							{#each block.head as cell, j (j)}
								<th
									scope="col"
									class={[
										'border border-line-strong bg-raised px-3 py-1.5 font-semibold text-fg',
										alignments[block.align[j]]
									]}
								>
									{@render spans(cell)}
								</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each block.rows as row, r (r)}
							<tr>
								{#each row as cell, j (j)}
									<td
										class={[
											'border border-line-strong px-3 py-1.5 text-fg-muted',
											alignments[block.align[j]]
										]}
									>
										{@render spans(cell)}
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else if block.ordered}
			<ol
				start={block.start === 1 ? undefined : block.start}
				class="my-3 list-decimal space-y-1 pl-6 text-fg-muted"
			>
				{@render listItems(block.items)}
			</ol>
		{:else}
			<ul class="my-3 list-disc space-y-1 pl-6 text-fg-muted">
				{@render listItems(block.items)}
			</ul>
		{/if}
	{/each}
{/snippet}

<!-- A long document scrolls here. Not every browser lets the keyboard focus a scroller, so this does. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	role="region"
	aria-label="Rendered Markdown"
	tabindex="0"
	class="h-full overflow-y-auto focus-visible:shadow-[inset_0_0_0_2px_rgb(82_168_255/0.5)]"
>
	<article class="mx-auto max-w-[860px] px-5 py-6 text-[14px] break-words sm:px-8 sm:py-8">
		{@render blockList(blocks)}
	</article>
</div>
