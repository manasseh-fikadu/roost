import { describe, expect, test } from 'bun:test';
import { edgeState, gap, layoutMap, maxWidth, minWidth, rowGap, type Group } from './layout.js';

function node(id: string, h = 100) {
	return { id, h };
}

/** Upstream nodes, the hub, then n peers. */
function groups(peers: number): Group[] {
	const ids = Array.from({ length: peers }, (_, i) => `peer${i}`);
	return [
		{
			id: 'top',
			link: 'up',
			nodes: [node('internet', 90), node('derp', 90)],
			edges: [
				{ to: 'internet', label: 'uplink', state: 'live' },
				{ to: 'derp', label: 'fra · 3.8 ms', state: 'idle' }
			]
		},
		{ id: 'hub', link: null, nodes: [node('hub', 130)], edges: [] },
		{
			id: 'peers',
			link: 'down',
			busLabel: 'tailnet',
			nodes: ids.map((id) => node(id)),
			edges: ids.map((id) => ({ to: id, label: 'direct', state: 'live' }))
		}
	];
}

describe('layoutMap', () => {
	test('centers each row, with the hub under its upstream nodes', () => {
		const width = 1000;
		const { boxes, height, paths } = layoutMap(width, groups(3), 'hub');
		const hub = boxes.get('hub')!;
		const internet = boxes.get('internet')!;
		const derp = boxes.get('derp')!;
		expect(hub.x + hub.w / 2).toBe(width / 2);
		expect(internet.y).toBe(18);
		expect(hub.y).toBe(internet.y + 90 + rowGap);
		expect(derp.x - (internet.x + internet.w)).toBe(gap);
		expect((internet.x + derp.x + derp.w) / 2).toBe(width / 2);
		expect(boxes.get('peer0')!.y).toBe(hub.y + 130 + rowGap);
		expect(height).toBe(boxes.get('peer0')!.y + 100 + 18);
		expect(paths.map((p) => p.key)).toEqual([
			'top:internet',
			'top:derp',
			'peers:peer0',
			'peers:peer1',
			'peers:peer2'
		]);
	});

	test('cards stay between the narrowest and widest', () => {
		for (const width of [200, 600, 3000]) {
			for (const b of layoutMap(width, groups(4), 'hub').boxes.values()) {
				expect(b.w).toBeLessThanOrEqual(maxWidth);
				// A map narrower than one card squeezes it.
				expect(b.w).toBeGreaterThanOrEqual(Math.min(minWidth, width));
			}
		}
	});

	test('wraps a row that doesn’t fit, and takes the second row around the side', () => {
		// Room for three cards a row.
		const width = 3 * minWidth + 2 * gap + 60;
		const { boxes, paths, busLabels } = layoutMap(width, groups(5), 'hub');
		const first = boxes.get('peer0')!;
		const fourth = boxes.get('peer3')!;
		expect(boxes.get('peer2')!.y).toBe(first.y);
		expect(fourth.y).toBe(first.y + 100 + rowGap);

		const hub = boxes.get('hub')!;
		const down = paths.find((p) => p.key === 'peers:peer0')!;
		const around = paths.find((p) => p.key === 'peers:peer3')!;
		// Straight down from the bottom of the hub, or out of its right side.
		expect(down.trunk.startsWith(`M${hub.x + hub.w / 2},${hub.y + hub.h}`)).toBe(true);
		expect(around.trunk.startsWith(`M${hub.x + hub.w},${hub.y + hub.h / 2}`)).toBe(true);
		expect(busLabels).toEqual([
			{
				key: 'peers',
				x: hub.x + hub.w / 2 + 8,
				y: hub.y + hub.h + rowGap / 2 - 6,
				text: 'tailnet',
				anchor: 'start'
			}
		]);
	});

	test('upstream lines meet above the hub, with labels between the rows', () => {
		const { boxes, paths } = layoutMap(1000, groups(1), 'hub');
		const hub = boxes.get('hub')!;
		const internet = boxes.get('internet')!;
		const up = paths.find((p) => p.key === 'top:internet')!;
		const bus = hub.y - rowGap / 2;
		expect(up.trunk).toBe(`M${hub.x + hub.w / 2},${hub.y}V${bus}H${internet.x + internet.w / 2}`);
		expect(up.ly).toBe((bus + internet.y + internet.h) / 2);
		expect(up.state).toBe('live');
		expect(paths.find((p) => p.key === 'top:derp')!.state).toBe('idle');
	});

	test('without the hub, there are cards but no lines', () => {
		const layout = layoutMap(1000, groups(2).slice(0, 1), 'hub');
		expect(layout.boxes.size).toBe(2);
		expect(layout.paths).toEqual([]);
	});

	test('an empty map has no height', () => {
		expect(layoutMap(1000, [], 'hub').height).toBe(0);
	});
});

describe('edgeState', () => {
	test.each([
		{ name: 'online flows', status: 'online' as const, want: 'live' },
		{ name: 'attention warns', status: 'attention' as const, want: 'warn' },
		{ name: 'offline is down', status: 'offline' as const, want: 'down' },
		{ name: 'unknown is idle', status: 'unknown' as const, want: 'idle' }
	])('$name', ({ status, want }) => {
		expect(edgeState(status)).toBe(want);
	});
});
