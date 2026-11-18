// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import d2 from 'astro-d2';
import starlightLinksValidator from 'starlight-links-validator';

const provider = (name, dir, existing) => ({
	label: name,
	items: [
		{ label: 'Overview', slug: `providers/${dir}` },
		...(existing ? [{ label: 'Existing setup', slug: `providers/${dir}/existing-deployment` }] : []),
		{ label: 'Walkthrough', slug: `providers/${dir}/walkthrough` },
		{ label: 'Limitations', slug: `providers/${dir}/limitations` },
	],
});

export default defineConfig({
	site: 'https://bret-operator.linogics.io',
	integrations: [
		// d2 must run before starlight so diagrams exist when pages render.
		d2({ layout: 'elk', pad: 20, theme: { default: '0', dark: '200' } }),
		starlight({
			title: 'bret',
			description: 'bret tests your Kubernetes backups by restoring them and running a check.',
			logo: { src: './src/assets/bret.png' },
			favicon: '/bret.png',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/linogics/bret-operator' }],
			editLink: { baseUrl: 'https://github.com/linogics/bret-operator/edit/main/docs/' },
			lastUpdated: true,
			plugins: [starlightLinksValidator({ errorOnRelativeLinks: true })],
			sidebar: [
				{
					label: 'Start',
					items: [
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
						provider('Velero', 'velero', true),
						provider('VolSync', 'volsync', true),
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
