// Find the sticky pawl comment (by marker) across ALL comment pages and either
// update it or create a new one. `api(method, path, payload)` is injected so the
// paging/create-vs-update logic is testable without GitHub. Returns 'updated' or
// 'created'.
export async function upsertComment({ api, owner, repo, prNumber, body, marker }) {
  let existing
  for (let page = 1; !existing; page++) {
    const batch = await api(
      'GET',
      `/repos/${owner}/${repo}/issues/${prNumber}/comments?per_page=100&page=${page}`,
    )
    existing = batch.find((c) => c.body && c.body.includes(marker))
    if (batch.length < 100) break // last page reached
  }
  if (existing) {
    await api('PATCH', `/repos/${owner}/${repo}/issues/comments/${existing.id}`, { body })
    return 'updated'
  }
  await api('POST', `/repos/${owner}/${repo}/issues/${prNumber}/comments`, { body })
  return 'created'
}
