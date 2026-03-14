import { apiFetch } from '$lib/server/api';
import type { DraftPayload, Meeting, ReportPayload } from '$lib/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, parent }) => {
	await parent();
	const meetingResponse = await apiFetch(`/v1/meetings/${params.id}`);
	const meeting = (await meetingResponse.json()) as Meeting;

	let draft: DraftPayload | null = null;
	let report: ReportPayload | null = null;

	if (meeting.status === 'review_required' || meeting.status === 'report_generating' || meeting.status === 'completed') {
		const draftResponse = await apiFetch(`/v1/meetings/${params.id}/draft`);
		draft = (await draftResponse.json()) as DraftPayload;
	}

	if (meeting.status === 'completed') {
		const reportResponse = await apiFetch(`/v1/meetings/${params.id}/report`);
		report = (await reportResponse.json()) as ReportPayload;
	}

	return { meeting, draft, report };
};
