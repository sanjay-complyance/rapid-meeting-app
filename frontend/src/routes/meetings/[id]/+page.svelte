<script lang="ts">
	import type { DraftPayload, Meeting, ReportPayload } from '$lib/types';

	let { data } = $props<{
		data: {
			meeting: Meeting;
			draft: DraftPayload | null;
			report: ReportPayload | null;
		};
	}>();

	const statusTone: Record<string, string> = {
		uploaded: 'pill',
		queued: 'pill warning',
		processing: 'pill warning',
		review_required: 'pill success',
		report_generating: 'pill warning',
		completed: 'pill success',
		failed: 'pill danger'
	};

	function shouldRefresh(status: string) {
		return ['queued', 'processing', 'report_generating'].includes(status);
	}
</script>

<svelte:head>
	{#if shouldRefresh(data.meeting.status)}
		<meta http-equiv="refresh" content="5" />
	{/if}
</svelte:head>

<section class="grid two">
	<div class="card stack">
		<div class="section-title">
			<h1>{data.meeting.title}</h1>
			<span class={statusTone[data.meeting.status] ?? 'pill'}>{data.meeting.status}</span>
		</div>
		<p class="muted">
			{#if data.meeting.source === 'fathom'}
				Source: Fathom
				{#if data.meeting.fathom_recording_id}
					| Recording {data.meeting.fathom_recording_id}
				{/if}
				{#if data.meeting.fathom_recorded_by_name}
					| Recorded by {data.meeting.fathom_recorded_by_name}
				{/if}
			{:else}
				File: {data.meeting.original_filename}
			{/if}
			{#if data.meeting.duration_seconds}
				| Duration {Math.round(data.meeting.duration_seconds)}s
			{/if}
		</p>
		{#if data.meeting.source === 'fathom' && (data.meeting.fathom_url || data.meeting.fathom_share_url)}
			<p class="muted">
				{#if data.meeting.fathom_url}
					<a href={data.meeting.fathom_url} target="_blank" rel="noreferrer">Open in Fathom</a>
				{/if}
				{#if data.meeting.fathom_share_url}
					{#if data.meeting.fathom_url}
						|
					{/if}
					<a href={data.meeting.fathom_share_url} target="_blank" rel="noreferrer">Open shared view</a>
				{/if}
			</p>
		{/if}

		{#if data.meeting.error_message}
			<p class="error">{data.meeting.error_message}</p>
		{/if}

		{#if data.meeting.status === 'review_required' || data.meeting.status === 'report_generating' || data.meeting.status === 'completed'}
			<div class="grid two">
				<a class="button link secondary" href={`/meetings/${data.meeting.id}/review`}>Open review</a>
				<a class="button link" href={`/meetings/${data.meeting.id}/report`}>Open report</a>
			</div>
		{/if}
	</div>

	<div class="card stack">
		<h2>Current state</h2>
		{#if data.meeting.status === 'queued'}
			<p class="muted">The worker has accepted the meeting and will start processing shortly.</p>
		{:else if data.meeting.status === 'processing'}
			<p class="muted">Audio normalization, transcription, and diarization are in progress.</p>
		{:else if data.meeting.status === 'review_required'}
			<p class="muted">The draft transcript is ready. Review flagged segments, rename speakers, then finalize.</p>
		{:else if data.meeting.status === 'report_generating'}
			<p class="muted">Final report generation is running from the latest reviewed transcript.</p>
		{:else if data.meeting.status === 'completed'}
			<p class="muted">The report is complete and ready to export as Markdown.</p>
		{:else}
			<p class="muted">Upload completed. Waiting for the next step.</p>
		{/if}

		{#if data.draft?.draft_preview_json}
			<div class="code">{data.draft.draft_preview_json}</div>
		{/if}
	</div>
</section>

{#if data.draft}
	<section class="card stack" style="margin-top: 1rem;">
		<h2>Draft at a glance</h2>
		<p class="muted">
			{data.draft.speaker_slots.length} speakers, {data.draft.segments.length} transcript segments.
		</p>
		{#if data.meeting.source === 'fathom' && data.meeting.fathom_summary_markdown}
			<div class="markdown">{data.meeting.fathom_summary_markdown}</div>
		{/if}
	</section>
{/if}

{#if data.report}
	<section class="card stack" style="margin-top: 1rem;">
		<h2>Latest Markdown report</h2>
		<div class="markdown">{data.report.markdown}</div>
	</section>
{/if}
