import type { LayoutServerLoad } from './$types';

const COOKIE_NAME = 'rapid_review_session';

export const load: LayoutServerLoad = async ({ cookies }) => {
	let reviewSessionId = cookies.get(COOKIE_NAME);
	if (!reviewSessionId) {
		reviewSessionId = crypto.randomUUID();
		cookies.set(COOKIE_NAME, reviewSessionId, {
			path: '/',
			httpOnly: false,
			sameSite: 'lax',
			maxAge: 60 * 60 * 24 * 30
		});
	}
	return { reviewSessionId };
};

