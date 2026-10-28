import type { UserAvailableChannel } from '../api/types'

/** Match channel metadata first, otherwise retain matching platform sections. */
export function filterAvailableChannels(channels: readonly UserAvailableChannel[], search: string): UserAvailableChannel[] {
  const query = search.trim().toLowerCase()
  if (!query) return [...channels]
  return channels.flatMap(channel => {
    if (channel.name.toLowerCase().includes(query) || (channel.description || '').toLowerCase().includes(query)) return [channel]
    const platforms = channel.platforms.filter(section =>
      section.platform.toLowerCase().includes(query) ||
      section.groups.some(group => group.name.toLowerCase().includes(query)) ||
      section.supported_models.some(model => model.name.toLowerCase().includes(query))
    )
    return platforms.length ? [{ ...channel, platforms }] : []
  })
}
