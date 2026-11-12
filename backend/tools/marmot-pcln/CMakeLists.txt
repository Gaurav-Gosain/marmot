add_executable(marmot-pcln marmot-pcln.cpp)
target_link_libraries(marmot-pcln PRIVATE MarmotPcln ${MARMOT_PCLN_LIBS})
