<script lang="ts">
	import type { DraftPayload } from '$lib/types';

	let { data, form } = $props<{
		data: { draft: DraftPayload };
		form?: { saved?: boolean; message?: string };
	}>();

	let showOnlyFlagged = $state(false);

	type DraftPreviewSummary = {
		diarization_mode?: string;
		speaker_count?: number;
		quality_flags?: {
			low_confidence_segments?: number;
			overlap_segments?: number;
			unclear_segments?: number;
		};
	};
	const LOW_CONFIDENCE_THRESHOLD = 0.45;

	function displayText(segment: DraftPayload['segments'][number]) {
		return segment.edited_text ?? segment.text;
	}

	function displaySpeaker(segment: DraftPayload['segments'][number]) {
		return segment.reviewed_speaker_slot_id ?? segment.speaker_slot_id;
	}

	function parseDraftPreview(preview?: string): DraftPreviewSummary | undefined {
		if (!preview) return undefined;
		try {
			return JSON.parse(preview) as DraftPreviewSummary;
		} catch {
			return undefined;
		}
	}

	function humanizeSpeakerLabel(label: string) {
		const match = label.match(/^SPEAKER_(\d+)$/);
		if (!match) return label;
		return `Speaker ${Number(match[1]) + 1}`;
	}

	function speakerDisplayNameById(speakerSlotId: string) {
		const speaker = data.draft.speaker_slots.find((item: DraftPayload['speaker_slots'][number]) => item.id === speakerSlotId);
		if (!speaker) return 'Unknown speaker';
		return speaker.assigned_name ?? humanizeSpeakerLabel(speaker.label);
	}

	function isFlagged(segment: DraftPayload['segments'][number]) {
		return (
			segment.confidence < LOW_CONFIDENCE_THRESHOLD ||
			segment.overlap_flag ||
			segment.unclear_flag ||
			segment.unclear_override
		);
	}

	function draftPreview() {
		return parseDraftPreview(data.draft.draft_preview_json);
	}

	function actionItems(): string[] {
		if (!data.draft.meeting.fathom_action_items_json) return [];
		try {
			const parsed = JSON.parse(data.draft.meeting.fathom_action_items_json) as Array<Record<string, unknown> | string>;
			return parsed
				.map((item) => {
					if (typeof item === 'string') return item;
					for (const key of ['description', 'text', 'title', 'action_item']) {
						const value = item[key];
						if (typeof value === 'string' && value.trim()) return value.trim();
					}
					return '';
				})
				.filter((item) => item.length > 0);
		} catch {
			return [];
		}
	}

	function flaggedSegmentCount() {
		return data.draft.segments.filter(isFlagged).length;
	}

	function includeSegment(segment: DraftPayload['segments'][number]) {
		if (!showOnlyFlagged) return true;
		return isFlagged(segment);
	}

	function formatTime(ms: number) {
		const totalSeconds = Math.floor(ms / 1000);
		const minutes = Math.floor(totalSeconds / 60)
			.toString()
			.padStart(2, '0');
		const seconds = (totalSeconds % 60).toString().padStart(2, '0');
		return `${minutes}:${seconds}`;
	}
</script>

<section class="card stack">
	<div class="section-title">
		<div>
			<h1>Review transcript</h1>
			<p class="muted">
				Rename diarized speakers, fix only the segments that need attention, then finalize the report.
			</p>
		</div>
		<div class="stack" style="justify-items: end;">
			{#if data.draft.lock_blocked}
				<span class="pill danger">Locked by another session</span>
			{:else}
				<span class="pill success">Lock active</span>
			{/if}
			{#if data.draft.latest_review_version}
				<span class="pill">Review v{data.draft.latest_review_version.version}</span>
			{/if}
		</div>
	</div>

	{#if form?.saved}
		<p class="pill success">Review saved.</p>
	{/if}
	{#if form?.message}
		<p class="error">{form.message}</p>
	{/if}

	<div class="grid two">
		<div class="card stack">
			<h2>Draft summary</h2>
			<div class="segment-meta">
				<span class="pill">Source {data.draft.meeting.source}</span>
				<span class="pill">Diarization {draftPreview()?.diarization_mode ?? 'unknown'}</span>
				<span class="pill">
					{draftPreview()?.speaker_count ?? data.draft.speaker_slots.length} detected speakers
				</span>
				<span class="pill warning">{flaggedSegmentCount()} flagged segments</span>
			</div>
			{#if draftPreview()?.quality_flags}
				<div class="segment-meta">
					<span class="pill">Low confidence {draftPreview()?.quality_flags?.low_confidence_segments ?? 0}</span>
					<span class="pill">Overlap {draftPreview()?.quality_flags?.overlap_segments ?? 0}</span>
					<span class="pill">Unclear {draftPreview()?.quality_flags?.unclear_segments ?? 0}</span>
				</div>
			{/if}
			{#if data.draft.meeting.source === 'fathom' && data.draft.meeting.fathom_summary_markdown}
				<div class="markdown">{data.draft.meeting.fathom_summary_markdown}</div>
			{/if}
			{#if data.draft.meeting.source === 'fathom' && actionItems().length > 0}
				<div class="stack">
					<h3>Imported action items</h3>
					<ul>
						{#each actionItems() as item}
							<li>{item}</li>
						{/each}
					</ul>
				</div>
			{/if}
		</div>
		<div class="card stack">
			<h2>Review tips</h2>
			<p class="muted">
				Start by renaming the diarized speakers, then scan only the flagged segments unless the transcript looks off.
			</p>
		</div>
	</div>

	<form method="POST" class="stack">
		<div class="card stack">
			<div class="section-title">
				<h2>Speaker names</h2>
				<label class="pill">
					<input bind:checked={showOnlyFlagged} type="checkbox" style="margin-right: 0.5rem;" />
					Show only flagged segments
				</label>
			</div>

			<div class="grid two">
				{#each data.draft.speaker_slots as speaker}
					<label class="field">
						<span>{humanizeSpeakerLabel(speaker.label)}</span>
						<input
							name={`speaker_name:${speaker.id}`}
							placeholder="Assign a name"
							value={speaker.assigned_name ?? ''}
							disabled={data.draft.lock_blocked}
						/>
					</label>
				{/each}
			</div>
		</div>

		<div class="segment-list">
			{#each data.draft.segments.filter(includeSegment) as segment}
				<div class="segment">
					<div class="segment-meta">
						<span class="pill">{formatTime(segment.start_ms)} - {formatTime(segment.end_ms)}</span>
						<span class="pill">{speakerDisplayNameById(displaySpeaker(segment))}</span>
						<span class:warning={segment.confidence < LOW_CONFIDENCE_THRESHOLD} class="pill">
							Confidence {segment.confidence.toFixed(2)}
						</span>
						{#if segment.overlap_flag}
							<span class="pill warning">Overlap</span>
						{/if}
						{#if segment.unclear_flag || segment.unclear_override}
							<span class="pill danger">Unclear</span>
						{/if}
					</div>

					<div class="grid two">
						<label class="field">
							<span>Speaker</span>
							<select name={`segment_speaker:${segment.id}`} disabled={data.draft.lock_blocked}>
								{#each data.draft.speaker_slots as speaker}
									<option value={speaker.id} selected={speaker.id === displaySpeaker(segment)}>
										{speaker.assigned_name ?? humanizeSpeakerLabel(speaker.label)}
									</option>
								{/each}
							</select>
						</label>

						<label class="field" style="align-content: end;">
							<span>Mark as unclear</span>
							<input
								name={`segment_unclear:${segment.id}`}
								type="checkbox"
								checked={segment.unclear_override}
								disabled={data.draft.lock_blocked}
							/>
						</label>
					</div>

					<label class="field">
						<span>Transcript</span>
						<textarea name={`segment_text:${segment.id}`} disabled={data.draft.lock_blocked}>{displayText(segment)}</textarea>
					</label>
				</div>
			{/each}
		</div>

		<div class="grid two">
			<button class="secondary" type="submit" formaction="?/save" disabled={data.draft.lock_blocked}>
				Save review
			</button>
			<button type="submit" formaction="?/finalize" disabled={data.draft.lock_blocked}>
				Save and finalize
			</button>
		</div>
	</form>
</section>
