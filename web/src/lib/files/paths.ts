// Paths on a machine, which are always absolute and use slashes.

/** Joins a folder and a name: "/" and "a" make "/a". */
export function joinPath(dir: string, name: string): string {
	return dir === '/' ? `/${name}` : `${dir}/${name}`;
}

/** The folder a path is in; the root's is the root. */
export function dirname(p: string): string {
	const i = p.lastIndexOf('/');
	return i <= 0 ? '/' : p.slice(0, i);
}

/** The last part of a path. */
export function basename(p: string): string {
	return p === '/' ? '/' : p.slice(p.lastIndexOf('/') + 1);
}

/** "/a/b/c" → [{name: "/", path: "/"}, {name: "a", path: "/a"}, …]. */
export function crumbs(p: string): Array<{ name: string; path: string }> {
	const out = [{ name: '/', path: '/' }];
	let acc = '';
	for (const part of p.split('/').filter(Boolean)) {
		acc += `/${part}`;
		out.push({ name: part, path: acc });
	}
	return out;
}

/** Shows paths under home as ~/…, the way a shell prompt does. */
export function tildify(p: string, home: string | null | undefined): string {
	if (!home || home === '/') {
		return p;
	}
	if (p === home) {
		return '~';
	}
	return p.startsWith(`${home}/`) ? `~${p.slice(home.length)}` : p;
}

/** "report.final.xlsx" → ["report.final", ".xlsx"]; ".tar.gz" stays together. */
export function splitExt(name: string): [string, string] {
	const m = /^(.+?)((?:\.tar)?\.[^.\s]{1,10})$/i.exec(name);
	return m && !name.startsWith('.') ? [m[1], m[2]] : [name, ''];
}
