import { useQuery } from '@tanstack/react-query'
import { fetchCatalog } from '@/lib/catalog'

export function useCatalog() {
  return useQuery({
    queryKey: ['catalog'],
    queryFn: ({ signal }) => fetchCatalog(signal),
    staleTime: 60_000,
    retry: 1,
  })
}
