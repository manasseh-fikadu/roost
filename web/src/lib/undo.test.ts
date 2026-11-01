import { describe, expect, test } from 'bun:test';
import { DeferQueue, type Schedule } from './undo.js';

/** A clock the test moves by hand. */
function manual() {
	let now = 0;
	const timers = new Set<{ at: number; fn: () => void }>();
	const schedule: Schedule = (fn, ms) => {
		const t = { at: now + ms, fn };
		timers.add(t);
		return () => timers.delete(t);
	};
	return {
		schedule,
		advance(ms: number) {
			now += ms;
			for (const t of [...timers].sort((a, b) => a.at - b.at)) {
				if (t.at <= now && timers.delete(t)) {
					t.fn();
				}
			}
		}
	};
}

describe('DeferQueue', () => {
	test('runs a thing once its time is up', () => {
		const clock = manual();
		const q = new DeferQueue(clock.schedule);
		const ran: string[] = [];
		q.add('a', () => ran.push('a'), 1000);
		clock.advance(999);
		expect(ran).toEqual([]);
		expect(q.has('a')).toBe(true);
		clock.advance(1);
		expect(ran).toEqual(['a']);
		expect(q.has('a')).toBe(false);
	});

	test('a thing taken back never runs', () => {
		const clock = manual();
		const q = new DeferQueue(clock.schedule);
		const ran: string[] = [];
		q.add('a', () => ran.push('a'), 1000);
		expect(q.cancel('a')).toBe(true);
		clock.advance(5000);
		expect(ran).toEqual([]);
	});

	test('taking back what is not waiting says so', () => {
		const q = new DeferQueue(manual().schedule);
		expect(q.cancel('nothing')).toBe(false);
	});

	test('a thing that already ran cannot be taken back', () => {
		const clock = manual();
		const q = new DeferQueue(clock.schedule);
		q.add('a', () => {}, 10);
		clock.advance(10);
		expect(q.cancel('a')).toBe(false);
	});

	test('adding under a waiting key does the earlier one now', () => {
		const clock = manual();
		const q = new DeferQueue(clock.schedule);
		const ran: string[] = [];
		q.add('a', () => ran.push('first'), 1000);
		q.add('a', () => ran.push('second'), 1000);
		expect(ran).toEqual(['first']);
		clock.advance(1000);
		expect(ran).toEqual(['first', 'second']);
	});

	test('flush does everything now, once', () => {
		const clock = manual();
		const q = new DeferQueue(clock.schedule);
		const ran: string[] = [];
		q.add('a', () => ran.push('a'), 1000);
		q.add('b', () => ran.push('b'), 2000);
		q.flush();
		expect(ran.sort()).toEqual(['a', 'b']);
		clock.advance(5000);
		expect(ran).toHaveLength(2);
	});
});
