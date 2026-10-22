// Where the network map puts its cards and the lines between them. Rows of
// cards are centered, and wrap when they don't fit. Every line starts at the
// hub: up to the row above it, down to the row below, or around the side,
// outside the rows in between, to a row further away, so that lines never
// cross a card.

import type { Status } from '../api/types.js';

/** How a line looks: live lines flow, and down or idle ones are dashed. */
export type EdgeState = 'live' | 'warn' | 'down' | 'idle';

/** A card on the map, h pixels tall. Its width comes from the layout. */
export interface MapNode {
	id: string;
	h: number;
}

/** The line from the hub to a node, and the label on it. */
export interface MapEdge {
	to: string;
	label: string;
	state: EdgeState;
}

/** How a row of nodes joins the hub. The hub's own row has no link. */
export type Link = 'up' | 'down' | 'side-left' | 'side-right' | null;

/** A row of nodes, which wraps into several if it doesn't fit. */
export interface Group {
	id: string;
	nodes: MapNode[];
	link: Link;
	edges: MapEdge[];
	/** A label on the line the group's nodes hang from, such as "tailnet". */
	busLabel?: string;
}

export interface Box {
	x: number;
	y: number;
	w: number;
	h: number;
}

/** The line from the hub to one node, as SVG paths. */
export interface MapPath {
	key: string;
	/** From the hub to the bus over the node's row. Nodes in a row share it. */
	trunk: string;
	/** From the bus to the node. */
	branch: string;
	state: EdgeState;
	label: string;
	/** Where the label goes. */
	lx: number;
	ly: number;
}

export interface BusLabel {
	key: string;
	x: number;
	y: number;
	text: string;
	anchor: 'start' | 'end';
}

export interface Layout {
	boxes: Map<string, Box>;
	height: number;
	paths: MapPath[];
	busLabels: BusLabel[];
}

/** Room at each side for lines that go around rows. */
export const gutter = 30;
/** Between cards in a row. */
export const gap = 14;
/** Between rows, where the lines and their labels go. */
export const rowGap = 58;
export const minWidth = 196;
export const maxWidth = 250;
/** Above the first row and below the last. */
const pad = 18;

/** How a line to something in a state looks. */
export function edgeState(status: Status): EdgeState {
	switch (status) {
		case 'online':
			return 'live';
		case 'attention':
			return 'warn';
		case 'offline':
			return 'down';
		case 'unknown':
			return 'idle';
	}
}

interface Row {
	group: Group;
	/** Which row of its group this is, from 0. */
	index: number;
	nodes: MapNode[];
	top: number;
	minX: number;
	maxX: number;
}

/** Lays the groups out, top to bottom, in a map width pixels wide. */
export function layoutMap(width: number, groups: Group[], hubId: string): Layout {
	const usable = Math.max(minWidth, width - gutter * 2);
	const perRow = Math.max(1, Math.floor((usable + gap) / (minWidth + gap)));
	const boxes = new Map<string, Box>();
	const rows: Row[] = [];
	let y = pad;
	for (const group of groups) {
		if (!group.nodes.length) {
			continue;
		}
		const cols = Math.min(perRow, group.nodes.length);
		const w = Math.min(maxWidth, (usable - gap * (cols - 1)) / cols);
		for (let i = 0; i < group.nodes.length; i += cols) {
			const nodes = group.nodes.slice(i, i + cols);
			const total = nodes.length * w + (nodes.length - 1) * gap;
			const minX = (width - total) / 2;
			const h = Math.max(...nodes.map((n) => n.h));
			nodes.forEach((n, j) => boxes.set(n.id, { x: minX + j * (w + gap), y, w, h }));
			rows.push({ group, index: i / cols, nodes, top: y, minX, maxX: minX + total });
			y += h + rowGap;
		}
	}
	const height = rows.length ? y - rowGap + pad : 0;
	const paths: MapPath[] = [];
	const busLabels: BusLabel[] = [];
	const hub = boxes.get(hubId);
	if (!hub) {
		return { boxes, height, paths, busLabels };
	}

	const hx = hub.x + hub.w / 2;
	const hubRow = rows.findIndex((r) => r.nodes.some((n) => n.id === hubId));
	rows.forEach((row, i) => {
		const { group } = row;
		if (!group.link) {
			return;
		}
		// Only the first row right under the hub hangs straight from it.
		let route: 'up' | 'down' | 'left' | 'right';
		if (group.link === 'up') {
			route = 'up';
		} else if (group.link === 'down' && row.index === 0 && i === hubRow + 1) {
			route = 'down';
		} else if (group.link === 'side-left') {
			route = 'left';
		} else {
			route = 'right';
		}

		// Lines around the side pass outside every row between the hub and this one.
		const between = rows.slice(Math.min(i, hubRow), Math.max(i, hubRow) + 1);
		const left = Math.max(6, Math.min(...between.map((r) => r.minX)) - gutter / 2);
		const right = Math.min(width - 6, Math.max(...between.map((r) => r.maxX)) + gutter / 2);

		const edges = new Map(group.edges.map((e) => [e.to, e]));
		for (const n of row.nodes) {
			const b = boxes.get(n.id);
			const e = edges.get(n.id);
			if (!b || !e) {
				continue;
			}
			const cx = b.x + b.w / 2;
			let trunk: string;
			let branch: string;
			let ly: number;
			if (route === 'up') {
				const bus = hub.y - rowGap / 2;
				trunk = `M${hx},${hub.y}V${bus}H${cx}`;
				branch = `M${cx},${bus}V${b.y + b.h}`;
				ly = (bus + b.y + b.h) / 2;
			} else {
				const bus = b.y - rowGap / 2;
				if (route === 'down') {
					trunk = `M${hx},${hub.y + hub.h}V${bus}H${cx}`;
				} else if (route === 'left') {
					trunk = `M${hub.x},${hub.y + hub.h / 2}H${left}V${bus}H${cx}`;
				} else {
					trunk = `M${hub.x + hub.w},${hub.y + hub.h / 2}H${right}V${bus}H${cx}`;
				}
				branch = `M${cx},${bus}V${b.y}`;
				ly = (bus + b.y) / 2;
			}
			paths.push({
				key: `${group.id}:${n.id}`,
				trunk,
				branch,
				state: e.state,
				label: e.label,
				lx: cx,
				ly
			});
		}

		if (group.busLabel && row.index === 0) {
			const text = group.busLabel;
			const above = row.top - rowGap / 2 - 6;
			if (route === 'down') {
				busLabels.push({
					key: group.id,
					x: hx + 8,
					y: hub.y + hub.h + rowGap / 2 - 6,
					text,
					anchor: 'start'
				});
			} else if (route === 'left') {
				busLabels.push({ key: group.id, x: left + 6, y: above, text, anchor: 'start' });
			} else if (route === 'right') {
				busLabels.push({ key: group.id, x: right - 6, y: above, text, anchor: 'end' });
			}
		}
	});
	return { boxes, height, paths, busLabels };
}
