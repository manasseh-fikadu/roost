<!--
	A Word document, drawn by docx-preview on dark paper. The page is drawn in
	an iframe that can't run script or load anything from the network, since
	a document is someone else's markup: it can carry links with javascript:
	URLs and CSS that would otherwise land on Roost's own origin, and Roost's
	origin can open shells on every machine. It fills its parent, and the
	document scrolls inside the frame.
-->
<script lang="ts">
	import { renderAsync } from 'docx-preview';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		/** The bytes of the .docx file. */
		data: ArrayBuffer;
	}

	let { data }: Props = $props();

	// Two walls, either of which holds alone. The sandbox has no allow-scripts, and the policy
	// allows images and fonts only as data: URLs (which is how docx-preview is asked to write
	// them), so nothing runs and nothing is fetched. It comes first, before any content.
	// allow-same-origin is only so this page can reach into the frame to draw and fit the
	// document; with no script in the frame, that lets nothing through. The colors are Roost's
	// raised surface and strong line, which the frame can't inherit.
	const SHELL = `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; font-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<style>
	html, body { margin: 0; background: #111111; color-scheme: light; }
	html { scrollbar-width: thin; scrollbar-color: #2e2e2e #111111; }
	.docx-wrapper { background: #111111 !important; padding: 28px 12px 40px !important; }
	.docx-wrapper > section.docx { box-shadow: 0 0 0 1px #2e2e2e, 0 12px 32px rgba(0, 0, 0, 0.55) !important; margin-bottom: 24px !important; }
	@media (max-width: 700px) { .docx-wrapper { padding: 8px 0 16px !important; } }
</style></head><body></body></html>`;

	// Links the document may keep. Anything else, such as javascript: or a relative path that
	// would resolve against the frame, loses its href.
	const SAFE_LINK = /^(https?:|mailto:|#)/i;

	// Parts of a page that have no place in a document. docx-preview draws none of them, so
	// these are only here in case the policy above is ever loosened.
	const UNSAFE = 'script, iframe, frame, object, embed, applet, link, meta, base, form';

	let frame = $state<HTMLIFrameElement>();
	let ready = $state(false);
	let status = $state<'loading' | 'done' | 'error'>('loading');
	let error = $state('');

	// Pages have a fixed paper width, so shrink them to fit a narrow screen instead of scrolling
	// sideways.
	function fit() {
		const doc = frame?.contentDocument;
		const wrapper = doc?.querySelector<HTMLElement>('.docx-wrapper');
		const page = doc?.querySelector<HTMLElement>('section.docx');
		if (!frame || !wrapper || !page) {
			return;
		}
		wrapper.style.zoom = '1';
		wrapper.style.zoom = String(Math.min(1, (frame.clientWidth - 16) / page.offsetWidth));
	}

	/** Takes anything that could act out of a drawn document, and sends its links to a new tab. */
	function sanitize(root: HTMLElement) {
		for (const el of root.querySelectorAll(UNSAFE)) {
			el.remove();
		}
		for (const el of root.querySelectorAll('*')) {
			for (const name of el.getAttributeNames()) {
				if (name.startsWith('on')) {
					el.removeAttribute(name);
				}
			}
		}
		for (const a of root.querySelectorAll('a')) {
			const href = (a.getAttribute('href') ?? '').trim();
			if (!SAFE_LINK.test(href)) {
				a.removeAttribute('href');
			} else if (!href.startsWith('#')) {
				a.setAttribute('rel', 'noopener noreferrer');
				if (!href.toLowerCase().startsWith('mailto:')) {
					a.setAttribute('target', '_blank');
				}
			}
		}
	}

	$effect(() => {
		const el = frame;
		if (!el) {
			return;
		}
		const observer = new ResizeObserver(() => fit());
		observer.observe(el);
		return () => observer.disconnect();
	});

	$effect(() => {
		const doc = ready ? frame?.contentDocument : null;
		if (!doc) {
			return;
		}
		const bytes = data;
		// renderAsync empties both containers, so neither may hold anything of ours. Each render
		// gets its own pair, which also means a render that finishes after the document changed
		// or the viewer closed draws into containers that are already gone.
		const styles = doc.createElement('div');
		const body = doc.createElement('div');
		doc.body.append(styles, body);
		let live = true;
		status = 'loading';
		renderAsync(bytes, body, styles, {
			inWrapper: true,
			breakPages: true,
			ignoreLastRenderedPageBreak: true,
			experimental: true,
			renderAltChunks: false,
			renderComments: false,
			// Images and fonts as data: URLs, so there are no blob URLs to revoke later.
			useBase64URL: true
		})
			.then(() => {
				if (!live) {
					return;
				}
				sanitize(body);
				fit();
				status = 'done';
			})
			.catch((err: unknown) => {
				if (!live) {
					return;
				}
				error = (err instanceof Error && err.message) || 'The document could not be read';
				status = 'error';
			});
		return () => {
			live = false;
			styles.remove();
			body.remove();
		};
	});
</script>

<div class="relative size-full bg-raised">
	<iframe
		bind:this={frame}
		title="Document preview"
		sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
		srcdoc={SHELL}
		onload={() => (ready = true)}
		class="size-full border-0 bg-raised"
	></iframe>
	{#if status === 'loading'}
		<div class="absolute inset-0 flex justify-center bg-raised p-8" role="status">
			<span class="sr-only">Reading the document</span>
			<Skeleton class="aspect-[1/1.414] w-full max-w-[640px] rounded-sm" />
		</div>
	{:else if status === 'error'}
		<div
			class="absolute inset-0 flex items-center justify-center bg-raised p-6 text-center text-[13px] text-red-fg"
			role="alert"
		>
			Couldn't render this document: {error}
		</div>
	{/if}
</div>
