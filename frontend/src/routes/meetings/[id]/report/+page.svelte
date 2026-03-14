<script lang="ts">
	import type { ReportPayload } from '$lib/types';

	let { data } = $props<{ data: { report: ReportPayload } }>();
</script>

<section class="grid two">
	<div class="card stack">
		<h1>{data.report.meeting.title}</h1>
		<p class="muted">Final RAPID Markdown report</p>
		<div class="markdown">{data.report.markdown}</div>
	</div>

	<div class="card stack">
		<h2>Structured JSON</h2>
		<pre class="code">{data.report.structured_json}</pre>
	</div>
</section>

