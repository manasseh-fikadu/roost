import { error } from '@sveltejs/kit';
import { dev } from '$app/env';
import type { PageLoad } from './$types';

// Every component of the design system on one page, for working on them.
// Only `make dev` serves it.
export const load: PageLoad = () => {
	if (!dev) {
		error(404, 'Not found');
	}
	return { title: 'Design system' };
};
