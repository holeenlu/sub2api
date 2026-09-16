## Where limits come from

Effective this project limits combine the key, user, group, model, gateway, and selected upstream. This public guide does not claim one fixed RPM, TPM, or concurrency limit for every account.

Check model allowlists and platform gates; user, key, group, and account-pool limits; request and file sizes; context and output limits; cache minimums; and dynamic upstream quotas.

Long reasoning, tools, and image generation need longer client timeouts. Streaming clients should separate connection and idle-read timeouts. A disconnected reader does not prove that the upstream request never ran.

For 429 responses, prefer `Retry-After`. Otherwise use exponential backoff with jitter and a maximum retry count. High-volume clients should also maintain a local queue and global concurrency cap.
