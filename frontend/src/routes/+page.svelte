<script lang="ts">
	import { env } from '$env/dynamic/public';

	const audioUploadsEnabled = (env.PUBLIC_ENABLE_AUDIO_UPLOADS ?? 'true').toLowerCase() !== 'false';
</script>

<section class="hero">
	{#if audioUploadsEnabled}
		<div class="card stack">
			<div>
				<h1>Upload a RAPID meeting recording</h1>
				<p class="muted">
					V1 accepts English audio uploads only. Meetings longer than 45 minutes are rejected in
					processing.
				</p>
			</div>

			<form method="POST" enctype="multipart/form-data" class="stack">
				<label class="field">
					<span>Meeting title</span>
					<input name="title" placeholder="Q2 pricing decision" required />
				</label>

				<label class="field">
					<span>Audio file</span>
					<input name="file" type="file" accept=".wav,.mp3,.m4a,audio/*" required />
				</label>

				<button type="submit">Upload and start processing</button>
			</form>
		</div>
	{:else}
		<div class="card stack">
			<div>
				<h1>Import a RAPID meeting</h1>
				<p class="muted">
					This deployment is configured for the Fathom-first path. Direct audio uploads are disabled
					until shared object storage is added.
				</p>
			</div>
			<p class="pill warning">Audio uploads disabled in this environment</p>
		</div>
	{/if}

	<aside class="card stack">
		<h2>Import from Fathom</h2>
		<p class="muted">
			For Fathom-recorded meetings, paste either the recording ID or the Fathom share link and skip the
			local transcription pipeline.
		</p>

		<form method="POST" action="?/fathom" class="stack">
			<label class="field">
				<span>Fathom recording ID or share link</span>
				<input
					name="recording_id"
					placeholder="123456789 or https://fathom.video/share/..."
					required
				/>
			</label>

			<button class="secondary" type="submit">Import from Fathom</button>
		</form>
	</aside>

	<aside class="card stack">
		<h2>Workflow</h2>
		<div class="stack">
			<span class="pill">1. Upload</span>
			<span class="pill">2. Process</span>
			<span class="pill">3. Review transcript</span>
			<span class="pill">4. Finalize RAPID report</span>
		</div>
		<p class="muted">
			The worker stores immutable source segments, then lets you rename speakers and edit only the
			segments that need correction.
		</p>
	</aside>
</section>
