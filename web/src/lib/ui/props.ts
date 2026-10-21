// Combines the props that bits-ui hands a trigger with a component's own.

type Props = Record<string, unknown>;

/**
 * Merges sets of element props into one. Later sets win, except that event
 * handlers (on…) run in order, so a trigger's handlers and the caller's both
 * run, and classes are kept side by side. Symbol keys are kept too: bits-ui
 * attaches a trigger's element to its component through one.
 */
export function mergeProps(...sets: Props[]): Props {
	const out: Record<PropertyKey, unknown> = {};
	for (const set of sets as Record<PropertyKey, unknown>[]) {
		for (const key of Reflect.ownKeys(set)) {
			const value = set[key];
			const prev = out[key];
			if (
				typeof key === 'string' &&
				key.startsWith('on') &&
				typeof prev === 'function' &&
				typeof value === 'function'
			) {
				out[key] = (...args: unknown[]) => {
					prev(...args);
					value(...args);
				};
			} else if (key === 'class' && prev && value) {
				out[key] = [prev, value];
			} else if (value !== undefined) {
				out[key] = value;
			}
		}
	}
	return out;
}
