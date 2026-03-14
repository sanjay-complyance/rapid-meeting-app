<script lang="ts">
	import '../app.css';

	let { data, children } = $props();
</script>

<div class="shell">
	<header class="topbar">
		<a class="brand" href="/">
			RAPID Assistant
			<small>English-only v1 workflow</small>
		</a>
		<span class="pill">Review session {data.reviewSessionId.slice(0, 8)}</span>
	</header>

	{@render children()}
</div>

