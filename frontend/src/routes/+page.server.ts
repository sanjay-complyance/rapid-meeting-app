import { apiFetch } from '$lib/server/api';
import { redirect, type Actions } from '@sveltejs/kit';

export const actions: Actions = {
	default: async ({ request }) => {
		const formData = await request.formData();
		const createResponse = await apiFetch('/v1/meetings', {
			method: 'POST',
			body: formData
		});
		const created = (await createResponse.json()) as { meeting_id: string };

		await apiFetch(`/v1/meetings/${created.meeting_id}/submit`, {
			method: 'POST'
		});

		throw redirect(303, `/meetings/${created.meeting_id}`);
	},
	fathom: async ({ request }) => {
		const formData = await request.formData();
		const input = String(formData.get('recording_id') ?? '').trim();
		if (!input) {
			throw redirect(303, '/');
		}
		const payload =
			input.startsWith('http://') || input.startsWith('https://')
				? { share_url: input }
				: { recording_id: input };

		const response = await apiFetch('/v1/integrations/fathom/import', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(payload)
		});
		const created = (await response.json()) as { meeting_id: string };

		throw redirect(303, `/meetings/${created.meeting_id}`);
	}
};
