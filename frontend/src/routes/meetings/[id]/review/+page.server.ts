import { apiFetch } from '$lib/server/api';
import type { DraftPayload } from '$lib/types';
import { fail, redirect, type Actions } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';

const COOKIE_NAME = 'rapid_review_session';

function buildPayload(formData: FormData) {
	const speakers = [];
	const segmentsMap = new Map<
		string,
		{ source_segment_id: string; edited_text: string | null; assigned_speaker_slot_id: string; unclear_override: boolean }
	>();

	for (const [key, value] of formData.entries()) {
		const textValue = typeof value === 'string' ? value : '';
		if (key.startsWith('speaker_name:')) {
			const speakerSlotId = key.split(':')[1];
			speakers.push({
				speaker_slot_id: speakerSlotId,
				assigned_name: textValue.trim() === '' ? null : textValue.trim()
			});
			continue;
		}

		if (key.startsWith('segment_text:')) {
			const sourceSegmentId = key.split(':')[1];
			const item = segmentsMap.get(sourceSegmentId) ?? {
				source_segment_id: sourceSegmentId,
				edited_text: null,
				assigned_speaker_slot_id: '',
				unclear_override: false
			};
			item.edited_text = textValue.trim() === '' ? null : textValue;
			segmentsMap.set(sourceSegmentId, item);
			continue;
		}

		if (key.startsWith('segment_speaker:')) {
			const sourceSegmentId = key.split(':')[1];
			const item = segmentsMap.get(sourceSegmentId) ?? {
				source_segment_id: sourceSegmentId,
				edited_text: null,
				assigned_speaker_slot_id: '',
				unclear_override: false
			};
			item.assigned_speaker_slot_id = textValue;
			segmentsMap.set(sourceSegmentId, item);
			continue;
		}

		if (key.startsWith('segment_unclear:')) {
			const sourceSegmentId = key.split(':')[1];
			const item = segmentsMap.get(sourceSegmentId) ?? {
				source_segment_id: sourceSegmentId,
				edited_text: null,
				assigned_speaker_slot_id: '',
				unclear_override: false
			};
			item.unclear_override = true;
			segmentsMap.set(sourceSegmentId, item);
		}
	}

	return {
		speakers,
		segments: Array.from(segmentsMap.values())
	};
}

export const load: PageServerLoad = async ({ params, parent }) => {
	const { reviewSessionId } = await parent();
	const response = await apiFetch(`/v1/meetings/${params.id}/draft`, {
		headers: { 'X-Review-Session': reviewSessionId }
	});
	return {
		draft: (await response.json()) as DraftPayload
	};
};

export const actions: Actions = {
	save: async ({ request, params, cookies }) => {
		const reviewSessionId = cookies.get(COOKIE_NAME);
		if (!reviewSessionId) {
			return fail(409, { message: 'Missing review session.' });
		}
		const formData = await request.formData();
		const payload = buildPayload(formData);
		if (payload.segments.length === 0) {
			return fail(400, { message: 'No segments found in review payload.' });
		}

		await apiFetch(`/v1/meetings/${params.id}/review`, {
			method: 'PATCH',
			headers: {
				'Content-Type': 'application/json',
				'X-Review-Session': reviewSessionId
			},
			body: JSON.stringify(payload)
		});

		return { saved: true };
	},
	finalize: async ({ request, params, cookies }) => {
		const reviewSessionId = cookies.get(COOKIE_NAME);
		if (!reviewSessionId) {
			return fail(409, { message: 'Missing review session.' });
		}
		const formData = await request.formData();
		const payload = buildPayload(formData);

		await apiFetch(`/v1/meetings/${params.id}/review`, {
			method: 'PATCH',
			headers: {
				'Content-Type': 'application/json',
				'X-Review-Session': reviewSessionId
			},
			body: JSON.stringify(payload)
		});

		await apiFetch(`/v1/meetings/${params.id}/finalize`, {
			method: 'POST',
			headers: { 'X-Review-Session': reviewSessionId }
		});

		throw redirect(303, `/meetings/${params.id}`);
	}
};
