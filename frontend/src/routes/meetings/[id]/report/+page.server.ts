import { apiFetch } from '$lib/server/api';
import type { ReportPayload } from '$lib/types';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params }) => {
	const response = await apiFetch(`/v1/meetings/${params.id}/report`);
	return {
		report: (await response.json()) as ReportPayload
	};
};

