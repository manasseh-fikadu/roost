import { describe, expect, test } from 'bun:test';
import { mergeProps } from './props.js';

describe('mergeProps', () => {
	test('runs every handler for an event, in order', () => {
		const calls: string[] = [];
		const merged = mergeProps(
			{ onclick: () => calls.push('trigger') },
			{ onclick: () => calls.push('caller') }
		);
		(merged.onclick as () => void)();
		expect(calls).toEqual(['trigger', 'caller']);
	});

	test('keeps every class', () => {
		expect(mergeProps({ class: 'a' }, { class: ['b', 'c'] }).class).toEqual(['a', ['b', 'c']]);
	});

	test('lets later props win', () => {
		expect(mergeProps({ type: 'submit', id: 'x' }, { type: 'button' })).toEqual({
			type: 'button',
			id: 'x'
		});
	});

	test('keeps symbol keys, such as attachments', () => {
		const attach = Symbol('attachment');
		const ref = () => {};
		const merged: Record<PropertyKey, unknown> = mergeProps({ [attach]: ref }, { id: 'x' });
		expect(merged[attach]).toBe(ref);
	});

	test('ignores undefined', () => {
		expect(mergeProps({ 'aria-label': 'Close' }, { 'aria-label': undefined })).toEqual({
			'aria-label': 'Close'
		});
	});
});
