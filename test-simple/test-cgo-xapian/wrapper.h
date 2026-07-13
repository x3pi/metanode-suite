#ifdef __cplusplus
extern "C" {
#endif

void CreateSampleDb(const char* dbpath);
int SearchDb(const char* dbpath);
void WriteDb(const char* dbpath, int start_idx);

#ifdef __cplusplus
}
#endif
