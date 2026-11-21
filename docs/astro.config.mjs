// @ts-check

import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';
import d2 from 'astro-d2';
import starlightLinksValidator from 'starlight-links-validator';
import d2Theme from './src/d2-theme.mjs';

/** @param {string} name @param {string} dir */
const provider = (name, dir) => ({
	label: name,
	items: [
		{ label: 'Overview', slug: `providers/${dir}` },
		{ label: 'Existing setup', slug: `providers/${dir}/existing-deployment` },
		{ label: 'Walkthrough', slug: `providers/${dir}/walkthrough` },
		{ label: 'Limitations', slug: `providers/${dir}/limitations` },
	],
});

export default defineConfig({
	site: 'https://bret-operator.linogics.io',
	integrations: [
		// d2 must run before starlight so diagrams exist when pages render.
		d2Theme(),
		d2({
			layout: 'tala',
			pad: 20,
			theme: { default: '0', dark: '200' },
			fonts: {
				regular: './src/assets/fonts/JetBrainsMono-Regular.ttf',
				italic: './src/assets/fonts/JetBrainsMono-Italic.ttf',
				bold: './src/assets/fonts/JetBrainsMono-Bold.ttf',
				semibold: './src/assets/fonts/JetBrainsMono-SemiBold.ttf',
			},
		}),
		starlight({
			title: 'bret',
			description: 'bret tests your Kubernetes backups by restoring them and running a check.',
			logo: { src: './src/assets/bret.png' },
			favicon: '/bret.png',
			customCss: ['./src/styles/theme.css'],
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/linogics/bret-operator' }],
			editLink: { baseUrl: 'https://github.com/linogics/bret-operator/edit/main/docs/' },
			lastUpdated: true,
			plugins: [starlightLinksValidator({ errorOnRelativeLinks: true })],
			sidebar: [
				{
					label: 'Start',
					items: [
						{ label: 'Introduction', link: '/' },
						{ label: 'Quickstart', slug: 'start/quickstart' },
						{ label: 'Installation', slug: 'start/installation' },
					],
				},
				{
					label: 'How it works',
					items: [
						{ label: 'Overview', slug: 'concepts/overview' },
						{ label: 'Lifecycle', slug: 'concepts/lifecycle' },
						{ label: 'Volumes', slug: 'concepts/volumes' },
					],
				},
				{
					label: 'Providers',
					items: [
						{ label: 'Compare providers', slug: 'providers' },
						{ label: 'Shared setup', slug: 'providers/shared-setup' },
						provider('Velero', 'velero'),
						provider('VolSync', 'volsync'),
						provider('Kopiur', 'kopiur'),
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Write a check', slug: 'guides/write-a-check' },
						{ label: 'Check catalog', slug: 'guides/check-catalog' },
						{ label: 'Test a check by hand', slug: 'guides/test-a-check' },
						{ label: 'Schedule restore tests', slug: 'guides/restore-tests' },
						{ label: 'Data and database', slug: 'guides/data-and-database' },
						{ label: 'Best practices', slug: 'guides/best-practices' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'API', slug: 'reference/api' },
						{ label: 'Failure reasons', slug: 'reference/failure-reasons' },
						{ label: 'Metrics and events', slug: 'reference/metrics-and-events' },
					],
				},
				{
					label: 'Contributing',
					items: [
						{ label: 'Development', slug: 'contributing/development' },
						{ label: 'Releasing', slug: 'contributing/releasing' },
					],
				},
			],
		}),
	],
});
