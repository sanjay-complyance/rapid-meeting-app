import { env } from '$env/dynamic/public';
import { error } from '@sveltejs/kit';

export function apiUrl(path: string): string {
	const baseUrl = env.PUBLIC_API_BASE_URL ?? 'http://localhost:8080';
	return `${baseUrl}${path}`;
}

export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
	const response = await fetch(apiUrl(path), init);
	if (!response.ok) {
		let message = `API request failed with ${response.status}`;
		try {
			const payload = await response.json();
			message = payload.error ?? payload.details ?? message;
		} catch {
			message = response.statusText || message;
		}
		throw error(response.status, message);
	}
	return response;
}
