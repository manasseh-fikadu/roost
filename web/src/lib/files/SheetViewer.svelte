<!--
	A spreadsheet, or a CSV or TSV file, as a grid you can read and move around
	in: a bar for the selected cell's address and value, row numbers and column
	letters that stay put, and a tab for each sheet. It fills its parent. Arrow
	keys, Home, End and PageUp/PageDown move between cells. Sheets are cut to
	the limits in sheet.ts, and a note says so. Reading a big workbook blocks
	the page while it runs, so a placeholder paints first.
-->
<script lang="ts">
	import type { WorkBook } from 'xlsx';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import {
		buildGrid,
		cellIndex,
		cellName,
		colName,
		moveFor,
		moveSelection,
		parseWorkbook,
		truncationNote,
		type Pos,
		type SheetCell
	} from '#lib/files/sheet.js';

	interface Props {
		/** The bytes of the file. */
		data: ArrayBuffer;
		/** The file's name, which tells a CSV or TSV file from a workbook. */
		name: string;
	}

	let { data, name }: Props = $props();

	/** The width of the column of row numbers, which is also what scrolling keeps clear of. */
	const NUMBERS_WIDTH = 52;
	const ROW_HEIGHT = 28;

	// Raw, since a workbook holds every cell and proxying it would only make reading it slow.
	let book = $state.raw<WorkBook | null>(null);
	let error = $state<string | null>(null);
	let sheet = $state(0);
	let sel = $state.raw<Pos | null>(null);
	// State because they're bound inside blocks, which come and go.
	let scroller = $state<HTMLDivElement>();
	let table = $state<HTMLTableElement>();

	const grid = $derived(book ? buildGrid(book.Sheets[book.SheetNames[sheet]]) : null);
	const note = $derived(grid ? truncationNote(grid) : null);
	const selCell = $derived(sel && grid ? grid.cells[sel.r]?.[sel.c] : undefined);
	const tableWidth = $derived(
		NUMBERS_WIDTH + (grid?.widths.reduce((total, w) => total + w, 0) ?? 0)
	);

	$effect(() => {
		const bytes = data;
		const file = name;
		book = null;
		error = null;
		sheet = 0;
		sel = null;
		// Reading blocks the page, so wait for a frame to paint the placeholder first.
		let timer: ReturnType<typeof setTimeout> | undefined;
		const frame = requestAnimationFrame(() => {
			timer = setTimeout(() => {
				try {
					book = parseWorkbook(bytes, file);
				} catch (err) {
					error = (err instanceof Error && err.message) || 'Unreadable spreadsheet';
				}
			});
		});
		return () => {
			cancelAnimationFrame(frame);
			clearTimeout(timer);
		};
	});

	// Whole class strings, one per look, since there can be 80,000 cells to give one to. Nothing for
	// a selection may change a cell's layout, such as `relative`: the browser would lay the whole
	// table out again for every move.
	const CELL =
		'h-7 cursor-cell overflow-hidden border-r border-b border-line px-2 text-ellipsis whitespace-nowrap focus-visible:rounded-none focus-visible:shadow-none aria-selected:bg-blue/10 aria-selected:outline-2 aria-selected:-outline-offset-2 aria-selected:outline-blue-fg';
	const CELL_CLASS = {
		text: `${CELL} text-left text-fg`,
		number: `${CELL} num text-right text-[12px] text-fg`,
		boolean: `${CELL} text-center text-fg`,
		// A literal error is a problem in the sheet. One from a formula is only that it couldn't be
		// worked out.
		error: `${CELL} text-left text-red-fg`,
		formulaError: `${CELL} text-left text-fg-subtle italic`
	};

	function cellClass(cell: SheetCell | undefined): string {
		if (cell?.kind === 'error') {
			return cell.formula ? CELL_CLASS.formulaError : CELL_CLASS.error;
		}
		return CELL_CLASS[cell?.kind ?? 'text'];
	}

	/** The cell on screen at a place, which isn't there if a merged range covers it. */
	function cellAt(r: number, c: number): HTMLElement | null {
		return table?.tBodies[0]?.rows[r]?.querySelector<HTMLElement>(`td[data-c="${c}"]`) ?? null;
	}

	// A selection changes three or four elements, so they're marked by hand. Having every cell
	// watch the selection instead makes each move wake all of them, which is 80,000 at the limit.
	// The cell that has the selection is the one Tab reaches, and with none yet it's the first.
	let marked: HTMLElement[] = [];
	$effect(() => {
		// Reading the grid is what makes this run again when a sheet's new cells replace the old.
		if (!grid || !table) {
			return;
		}
		const at = sel ?? { r: 0, c: 0 };
		const cell = cellAt(at.r, at.c);
		for (const el of marked) {
			el.removeAttribute('aria-selected');
			el.removeAttribute('data-active');
			if (el.tagName === 'TD') {
				el.tabIndex = -1;
			}
		}
		marked = [];
		if (cell) {
			cell.tabIndex = 0;
			marked.push(cell);
		}
		if (sel) {
			cell?.setAttribute('aria-selected', 'true');
			const col = table.tHead?.rows[0]?.cells[sel.c + 1];
			const row = table.tBodies[0]?.rows[sel.r]?.cells[0];
			for (const el of [col, row]) {
				el?.setAttribute('data-active', '');
				if (el) {
					marked.push(el);
				}
			}
		}
	});

	function showSheet(i: number) {
		sheet = i;
		sel = null;
		scroller?.scrollTo(0, 0);
	}

	/** The cell an event came from. Cells carry their place so they can share one handler. */
	function posOf(el: EventTarget | null): Pos {
		const { r, c } = (el as HTMLElement).dataset;
		return { r: Number(r), c: Number(c) };
	}

	// Focus selects, so a click, Tab and the arrow keys all end up here.
	function select(e: Event) {
		const pos = posOf(e.currentTarget);
		if (sel?.r !== pos.r || sel.c !== pos.c) {
			sel = pos;
		}
	}

	function onKeydown(e: KeyboardEvent) {
		if (!grid || e.altKey || e.shiftKey) {
			return;
		}
		const move = moveFor(e.key, e.ctrlKey || e.metaKey);
		if (!move) {
			return;
		}
		e.preventDefault();
		const page = Math.max(1, Math.floor((scroller?.clientHeight ?? 0) / ROW_HEIGHT) - 2);
		const to = moveSelection(grid, posOf(e.currentTarget), move, page);
		sel = to;
		cellAt(to.r, to.c)?.focus();
	}
</script>

<div class="flex size-full flex-col bg-surface">
	{#if error}
		<div
			class="flex min-h-0 flex-1 items-center justify-center p-6 text-center text-[13px] text-red-fg"
			role="alert"
		>
			Couldn't read this spreadsheet: {error}
		</div>
	{:else if book && grid}
		<div
			class="flex h-9 shrink-0 items-center gap-2 border-b border-line px-3 font-mono text-[12px]"
		>
			<span class="w-14 shrink-0 text-fg-subtle">{sel ? cellName(sel) : ''}</span>
			<span class="h-4 w-px bg-line-strong" aria-hidden="true"></span>
			<span class="min-w-0 flex-1 truncate text-fg" title={selCell?.text}>
				{#if !sel}
					<span class="text-fg-faint">Select a cell to see its value</span>
				{:else if selCell?.formula}
					<span class="text-blue-fg">={selCell.formula}</span>
				{:else}
					{selCell?.text ?? ''}
				{/if}
			</span>
			<span
				class="hidden shrink-0 text-fg-faint sm:inline"
				title="{grid.totalRows.toLocaleString()} rows by {grid.totalCols.toLocaleString()} columns"
			>
				{grid.totalRows.toLocaleString()} × {grid.totalCols.toLocaleString()}
			</span>
		</div>
		<div bind:this={scroller} class="min-h-0 flex-1 scroll-pt-7 scroll-pl-[52px] overflow-auto">
			{#if grid.rows === 0 || grid.cols === 0}
				<div class="p-8 text-center text-[13px] text-fg-subtle">This sheet is empty.</div>
			{:else}
				<table
					bind:this={table}
					role="grid"
					aria-label={book.SheetNames.length > 1 ? book.SheetNames[sheet] : name}
					class="border-separate border-spacing-0 text-[12.5px]"
					style:table-layout="fixed"
					style:width="{tableWidth}px"
				>
					<colgroup>
						<col style:width="{NUMBERS_WIDTH}px" />
						{#each grid.widths as w, c (c)}
							<col style:width="{w}px" />
						{/each}
					</colgroup>
					<thead>
						<tr>
							<th
								scope="col"
								class="sticky top-0 left-0 z-20 h-7 border-r border-b border-line-strong bg-raised"
							>
								<span class="sr-only">Row</span>
							</th>
							{#each { length: grid.cols }, c}
								<th
									scope="col"
									class="sticky top-0 z-10 h-7 border-r border-b border-line-strong bg-raised px-1 text-center font-mono text-[11px] font-normal text-fg-subtle data-active:bg-hover data-active:text-fg"
								>
									{colName(c)}
								</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each grid.cells as row, r (r)}
							<tr>
								<th
									scope="row"
									class="sticky left-0 z-10 h-7 border-r border-b border-line-strong bg-raised px-1.5 text-right num text-[11px] font-normal text-fg-faint data-active:bg-hover data-active:text-fg"
								>
									{r + 1}
								</th>
								{#each { length: grid.cols }, c}
									<!-- A merged range is one cell, drawn where it starts. -->
									{#if !grid.covered.has(cellIndex(grid.cols, r, c))}
										<td
											role="gridcell"
											tabindex="-1"
											data-r={r}
											data-c={c}
											rowspan={grid.spans.get(cellIndex(grid.cols, r, c))?.r}
											colspan={grid.spans.get(cellIndex(grid.cols, r, c))?.c}
											title={row[c] && row[c].text.length > 24 ? row[c].text : undefined}
											onclick={select}
											onfocusin={select}
											onkeydown={onKeydown}
											class={cellClass(row[c])}>{row[c]?.text}</td
										>
									{/if}
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
			{#if note}
				<div class="sticky left-0 px-3 py-2 text-[12px] text-fg-subtle">{note}</div>
			{/if}
		</div>
		{#if book.SheetNames.length > 1}
			<div
				role="group"
				aria-label="Sheets"
				class="flex h-9 shrink-0 items-center gap-1 overflow-x-auto border-t border-line bg-canvas px-2"
			>
				{#each book.SheetNames as sheetName, i (sheetName)}
					<button
						type="button"
						aria-pressed={i === sheet}
						onclick={() => showSheet(i)}
						class={[
							'h-6 shrink-0 rounded-md px-2.5 text-[12px] transition-colors',
							i === sheet
								? 'bg-hover text-fg shadow-[inset_0_0_0_1px_var(--color-line-strong)]'
								: 'text-fg-subtle hover:text-fg'
						]}
					>
						{sheetName}
					</button>
				{/each}
			</div>
		{/if}
	{:else}
		<div class="min-h-0 flex-1" role="status">
			<span class="sr-only">Reading the spreadsheet</span>
			<Skeleton class="size-full rounded-none" />
		</div>
	{/if}
</div>
