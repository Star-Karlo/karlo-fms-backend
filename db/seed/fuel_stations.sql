-- db/seed/fuel_stations.sql — the SPBU reference data, pre-fetched.
--
-- WHY THIS EXISTS. cmd/import-fuel pulls this from Overpass, which takes
-- several minutes and needs both internet and database access from the SAME
-- machine. Splitting them means the slow, internet-dependent half can run
-- anywhere (it already has) and the database half is a single fast load from
-- whichever network can actually reach RDS.
--
-- Regenerate by running cmd/import-fuel against any database and re-dumping.
-- Idempotent: existing rows are updated rather than duplicated.

BEGIN;

CREATE TEMP TABLE fuel_station_load (
  osm_type text, osm_id bigint, brand text, brand_raw text,
  name text, lat double precision, lon double precision, island text
) ON COMMIT DROP;

COPY fuel_station_load (osm_type, osm_id, brand, brand_raw, name, lat, lon, island) FROM stdin;
node	29392017	pertamina	Pertamina	Pertamina	-6.8876837	107.6020333	jawa
node	29392645	pertamina	Pertamina	Pertamina	-6.9070048	107.5858523	jawa
node	32520380	pertamina	Pertamina	Pertamina	-6.8997316	107.6343231	jawa
node	32529107	pertamina	Pertamina	Pertamina	-6.8806043	107.5987095	jawa
node	32529115	pertamina	Pertamina	Pertamina	-6.9185944	107.5840572	jawa
node	274750656	pertamina	Pertamina	Pertamina	-8.3525551	114.6039488	jawa
node	274813078	pertamina	Pertamina	Pertamina	-8.3247965	114.5409782	jawa
node	298739945	pertamina	Pertamina	SPBU Pertamina 54.661.26	-8.0764533	112.2291461	jawa
node	298740781	other	PB3	PB3	-8.0830207	112.1211865	jawa
node	321964917	pertamina	SPBU	SPBU	-6.371344	106.8965435	jawa
node	407492040	pertamina	PT Pertamina	SPBU	-6.1324155	106.9336357	sumatra
node	408183254	pertamina	Independent	SPBU	-6.129402	106.906899	sumatra
node	420803368	other	Pertamine	Pertamine	-7.572428	112.72602	jawa
node	420803369	other	Pertamine	Pertamine	-7.5937386	112.7627016	jawa
node	429673446	pertamina	Pertamina	Pertamina	-7.6526614	112.6876929	jawa
node	429673764	pertamina	Pertamina	Pertamina	-7.5902524	112.7455896	jawa
node	429979882	pertamina	Pertamina	Pertamina	-7.2634443	112.7521186	jawa
node	445615315	other	\\N	\\N	-7.6267097	108.2626144	jawa
node	445615336	pertamina	SPBU Ciokong	SPBU Ciokong	-7.6756058	108.5883345	jawa
node	445615364	other	\\N	\\N	-7.7217789	108.474582	jawa
node	445615366	pertamina	SPBU Parigi	SPBU Parigi	-7.6961289	108.5017652	jawa
node	445615824	pertamina	SPBU Babakan	SPBU Babakan	-7.6739227	108.6809261	jawa
node	445615828	pertamina	SPBU Putrapinggan	SPBU Putrapinggan	-7.6713822	108.7050489	jawa
node	445615855	other	\\N	\\N	-7.4864035	108.819077	jawa
node	448090100	other	\\N	\\N	-7.535233	109.293631	jawa
node	448090101	other	\\N	\\N	-7.647606	109.042871	jawa
node	448090102	other	\\N	\\N	-7.6627573	109.0908015	jawa
node	448090104	pertamina	SPBU Pertamina 44.534.02	SPBU Pertamina 44.534.02	-7.4514004	109.4711694	jawa
node	448090108	other	\\N	\\N	-7.6206223	109.2500162	jawa
node	448090109	pertamina	Pertamina	SPBU Blambangan	-7.3963354	109.6393055	jawa
node	448090110	other	\\N	\\N	-7.27064	109.730867	jawa
node	448090112	other	\\N	\\N	-7.6531508	109.1173377	jawa
node	448090113	other	\\N	\\N	-7.508653	109.360455	jawa
node	448090118	other	\\N	\\N	-7.399095	109.677216	jawa
node	448090120	other	\\N	\\N	-7.421571	109.97183	jawa
node	448090121	pertamina	SPBU Siyono Kertek	SPBU Siyono Kertek	-7.379492	109.9514661	jawa
node	448090122	other	\\N	\\N	-7.3225484	109.9122195	jawa
node	448834547	other	\\N	\\N	-7.602417	110.225273	jawa
node	448834548	pertamina	Pertamina	SPBU Pertamina 44.555.10 Sidorejo	-7.7609117	110.2649279	jawa
node	448834565	pertamina	SPBU Ngadirojo	SPBU Ngadirojo	-7.8150813	110.9826504	jawa
node	448834569	other	\\N	\\N	-7.572409	110.121374	jawa
node	448887712	other	\\N	\\N	-7.872454	111.347877	jawa
node	448887783	other	\\N	\\N	-7.852866	111.278055	jawa
node	448887804	other	\\N	\\N	-7.825999	111.187012	jawa
node	448887822	other	\\N	\\N	-7.901133	111.407664	jawa
node	448887838	pertamina	SPBU Sidoharjo	SPBU Sidoharjo	-7.8270486	111.0499223	jawa
node	448887882	other	\\N	\\N	-7.867398	111.39462	jawa
node	449839967	other	\\N	\\N	-8.2281338	112.8379833	jawa
node	449839968	other	\\N	\\N	-8.2308041	112.3353631	jawa
node	452888072	other	\\N	\\N	-8.2932746	113.6918513	jawa
node	452888089	pertamina	Pertamina	Pertamina	-8.2989571	113.9949414	jawa
node	660844547	pertamina	Pertamina	Pertamina	-7.364953	112.7636764	jawa
node	762015091	pertamina	Pertamina	Pertamina	-6.5272514	106.7644442	jawa
node	1057058546	pertamina	Pertamina	SPBU 34-16107	-6.6188463	106.8132991	jawa
node	1107877728	pertamina	Pertamina	Pertamina	-6.9165401	107.6184175	jawa
node	1255595250	pertamina	Pertamina	Pertamina	-6.3454943	106.7539733	jawa
node	1323725296	pertamina	SPBU Margonda	SPBU Margonda	-6.3838536	106.8303387	jawa
node	1354094577	pertamina	SPBU Beji	SPBU Beji	-6.3741972	106.8135231	jawa
node	1358383726	pertamina	Pertamina	SPBU Pertamina 44.555.06 Godean	-7.7735077	110.3151245	jawa
node	1360220733	pertamina	Pertamina	SPBU Pertamina 44.555.19 Mlati Baru	-7.7389926	110.3625989	jawa
node	1370745608	other	\\N	\\N	-7.5778047	110.2742297	jawa
node	1432872281	pertamina	SPBU Sawangan	SPBU Sawangan	-6.3967594	106.8057737	jawa
node	1451908147	pertamina	Pertamina	Pertamina	-6.9366919	107.7628755	jawa
node	1556616314	pertamina	Pertamina	SPBU 34-16115	-6.5947641	106.7840646	jawa
node	1631214033	pertamina	Pertamina	SPBU Pertamina 54.661.27	-8.061683	112.0637578	jawa
node	1654007743	pertamina	Pertamina	SPBU Pertamina 44.551.08 Jogokariyan	-7.8245175	110.3672995	jawa
node	1654016151	pertamina	Pertamina	SPBU Pertamina 44.551.01 Dukuh	-7.8190266	110.3559127	jawa
node	1654016379	pertamina	Pertamina	SPBU Pertamina 44.551.04 Bugisan	-7.8123508	110.3514093	jawa
node	1663629332	pertamina	SPBU Sidomoyo	SPBU Sidomoyo	-7.7546342	110.320303	jawa
node	1697075891	pertamina	Pertamina	SPBU 54.681.19	-8.2102467	113.7008029	jawa
node	1697110806	pertamina	Pertamina	SPBU Pertamina Tembaan	-8.1734851	113.7038271	jawa
node	1704548411	pertamina	Pertamina	SPBU	-7.9028899	112.6611771	jawa
node	1711312275	pertamina	Pertamina	\\N	-6.5356301	106.8069993	jawa
node	1716662294	pertamina	Pertamina	Pertamina	-8.1329166	113.8068402	jawa
node	1716662399	other	\\N	\\N	-8.1711714	113.7933379	jawa
node	1723598228	pertamina	PERTAMINA	SPBU Pertamina  Kranjingan	-8.1559644	113.7007216	jawa
node	1725288871	pertamina	PERTAMINA	SPBU	-8.1261022	113.3773268	jawa
node	1733824782	pertamina	PERTAMINA	\\N	-8.1687591	113.6929228	jawa
node	1743490574	pertamina	Pertamina	\\N	-6.6372957	106.8437806	jawa
node	1752235442	pertamina	SPBU Leuwiliang	SPBU Leuwiliang	-6.5732701	106.6308059	jawa
node	1754634591	pertamina	PERTAMINA	SPBU Pertamina Kebonsari	-8.190876	113.6937902	jawa
node	1766443414	other	\\N	\\N	-8.0844783	112.3133319	jawa
node	1766443429	other	\\N	\\N	-8.0916708	112.3249324	jawa
node	1766473105	pertamina	Pertamina	SPBU Pertamina 54.661.25 Jl.Kalimantan	-8.1073624	112.1801149	jawa
node	1766491099	pertamina	Pertamina	Pertamina	-8.0985111	112.1452295	jawa
node	1767108510	pertamina	Pertamina	Pertamina	-7.5059006	110.2046821	jawa
node	1767321271	pertamina	Pertamina	SPBU Pertamina 54.661.12	-8.0794668	112.2419576	jawa
node	1769198150	pertamina	Pertamina	SPBU Pertamina 44.555.05 Prujakan	-7.7361861	110.3945182	jawa
node	1769202454	pertamina	Pertamina	SPBU Pertamina 44.555.08 Ngaglik	-7.7235407	110.401344	jawa
node	1775532567	pertamina	Pertamina	SPBU	-7.3190103	108.1992741	jawa
node	1780657271	other	\\N	\\N	-7.1831846	110.4253727	jawa
node	1781964169	pertamina	Pertamina	Pertamina	-6.9669124	107.6814323	jawa
node	1781984739	pertamina	Pertamina	Pertamina	-6.8849894	107.5195617	jawa
node	1783241416	pertamina	SPBU Pertamina	SPBU Pertamina	-6.443295	107.0419955	jawa
node	1789889479	other	\\N	\\N	-6.7883697	108.6434231	jawa
node	1789889688	other	\\N	\\N	-6.8038735	108.6711135	jawa
node	1795213872	pertamina	Pertamina	SPBU Pertamina 44.574.22 Cawas	-7.7584522	110.6943042	jawa
node	1795220386	pertamina	Pertamina	SPBU 44.575.08 Kedungjambal	-7.747541	110.7647595	jawa
node	1806095110	pertamina	SPBU 54.672.12	SPBU 54.672.12	-7.7930386	113.2985357	jawa
node	1806107890	other	\\N	\\N	-7.874385	113.9148536	jawa
node	1807560548	other	\\N	\\N	-6.8833246	108.4944089	jawa
node	1807566068	pertamina	Pertamina	SPBU Pertamina 34-45516 Manislor	-6.9009228	108.4904876	jawa
node	1810330868	other	\\N	\\N	-6.5433215	107.7259923	jawa
node	1810561713	other	\\N	\\N	-6.7208713	108.2996205	jawa
node	1836612609	pertamina	Pertamina	SPBU Pertamina 44.557.08 Payak	-7.8322142	110.461162	jawa
node	1836620000	pertamina	Pertamina	SPBU Pertamina 44.557.04 Tegalyoso	-7.8280742	110.4475039	jawa
node	1836630400	pertamina	Pertamina	SPBU Pertamina 44.557.02 Piyungan	-7.8205504	110.480692	jawa
node	1847844689	pertamina	Pertamina	SPBU Ancaran	-6.9731776	108.5228871	jawa
node	1847959752	pertamina	Pertamina	SPBU Pertamina 44.552.01 Mulungan	-7.7326602	110.3634275	jawa
node	1848219193	pertamina	Pertamina	Pertamina	-6.7521109	106.7993428	jawa
node	1848293575	pertamina	Pertamina	Pertamina	-6.7673758	106.7967866	jawa
node	1848304237	pertamina	Pertamina	\\N	-6.796402	106.7778207	jawa
node	1864266359	pertamina	SPBU Pertamina 54.681.20	SPBU Pertamina 54.681.20	-8.2773246	113.6404969	jawa
node	1872643817	pertamina	Pertamina	\\N	-7.6072675	109.472724	jawa
node	1873047349	pertamina	Pertamina	SPBU 44.505.08	-7.1364297	110.4053214	jawa
node	1873047352	pertamina	Pertamina	Pertamina	-7.1481491	110.4077956	jawa
node	1884241147	pertamina	PT Pertamina	SPBU 44.501.12	-6.9746686	110.4137551	jawa
node	1884688755	pertamina	Pertamina	SPBU Klahang	-7.4463211	109.3150483	jawa
node	1895864289	pertamina	Pertamina	SPBU Pertamina 44.551.10 Mantup	-7.8206421	110.4215837	jawa
node	1930142130	pertamina	Pertamina	Pertamina	-6.9949911	107.8534161	jawa
node	1946134221	pertamina	Pertamina	Pertamina	-6.994982	107.8526787	jawa
node	1954844543	pertamina	Pertamina	Pertamina	-6.966744	107.8217095	jawa
node	1954844568	other	SPBE	SPBE	-6.9759495	107.8388089	jawa
node	1954844679	pertamina	Pertamina	Pertamina	-6.9662688	107.8183436	jawa
node	1954865220	pertamina	Pertamina	Pertamina	-6.9698444	107.8268006	jawa
node	1956516948	pertamina	Pertamina	Pertamina	-6.8689864	107.6208132	jawa
node	1987864817	pertamina	Pertamina	\\N	-5.8820294	110.4398169	jawa
node	1994705049	other	\\N	\\N	-6.2976447	107.1474729	jawa
node	1995441372	other	\\N	\\N	-6.2953066	107.1462311	jawa
node	1996265822	other	\\N	\\N	-6.3100682	107.1649341	jawa
node	1997186267	pertamina	Pertamina	SPBU 34-17504	-6.293699	107.148342	jawa
node	1997191731	pertamina	Pertamina	\\N	-6.2812218	107.1647785	jawa
node	2004741428	other	POM BENSIN	POM BENSIN	-7.7077914	109.0055785	jawa
node	2016380280	pertamina	Pertamina	Pertamina	-6.8890673	107.5816606	jawa
node	2027720866	pertamina	Pertamina	SPBU Pertamina	-6.4051845	106.7456337	jawa
node	2049289684	pertamina	Pertamina	SPBU 44.572.03	-7.3849968	111.0883844	jawa
node	2050877609	pertamina	SPBU Limo	SPBU Limo	-6.3893005	106.7687355	jawa
node	2060907458	other	Fuel Station Gondoriyo	Fuel Station Gondoriyo	-7.2736151	110.3747202	jawa
node	2070373386	other	\\N	\\N	-6.7131923	111.1596068	jawa
node	2072186950	other	\\N	\\N	-6.7969591	110.9511517	jawa
node	2073000397	other	\\N	\\N	-6.3997202	106.7719296	jawa
node	2073000452	other	\\N	\\N	-6.4008407	106.771367	jawa
node	2088036721	pertamina	Pertamina	SPBU Mastrip	-8.1599234	113.7200019	jawa
node	2130372735	pertamina	Pertamina	SPBU Bapangan	-6.6034619	110.6787775	jawa
node	2130416514	pertamina	Pertamina	SPBU Wedelan	-6.5174905	110.7793607	jawa
node	2149754352	pertamina	SPBU Papringan	SPBU Papringan	-6.7683155	110.7752137	jawa
node	2153941874	pertamina	Pertamina	\\N	-6.3430341	107.3405942	jawa
node	2154038580	pertamina	Pertamina	SPBU	-6.3674681	107.3682364	jawa
node	2155587775	other	\\N	\\N	-8.3591298	113.4720809	jawa
node	2155598874	pertamina	SPBU Pertamina Tawangalun	SPBU Pertamina Tawangalun	-8.1971721	113.6347301	jawa
node	2155605619	pertamina	SPBU Pertamina	SPBU Pertamina	-8.1918879	113.6466356	jawa
node	2160194797	other	\\N	\\N	-7.2736153	110.4557518	jawa
node	2271402995	other	POM	POM	-6.5534016	106.807647	jawa
node	2279394309	pertamina	Pertamina	SPBU Cipanggulaan 34-43311	-6.8218517	106.7639421	jawa
node	2320237854	pertamina	Pertamina	SPBU Kalimanah	-7.4176681	109.339834	jawa
node	2322672467	pertamina	Pertamina	SPBU Pertamina	-8.1968657	113.6522065	jawa
node	2341423183	pertamina	Pertamina	SPBU Pertamina 44.574.24 Solo Balapan	-7.5573234	110.8234246	jawa
node	2344343593	pertamina	Pertamina	SPBU	-7.2025324	107.8898141	jawa
node	2344420830	pertamina	Pertamina	SPBU	-7.1797746	107.8904506	jawa
node	2344420832	pertamina	Pertamina	SPBU	-7.1901089	107.8986825	jawa
node	2345307522	pertamina	Pertamina	Pertamina	-7.381781	112.7504654	jawa
node	2347667295	other	Pom Bensin Tunggorono	\\N	-7.5447626	112.2168766	jawa
node	2348679818	pertamina	Pertamina	\\N	-7.4393234	109.2473667	jawa
node	2360646864	pertamina	Pertamina	SPBU Pertamina 44.551.06 Kadipiro	-7.8005099	110.3451438	jawa
node	2360646879	pertamina	Pertamina	SPBU Pertamina 44.551.12 Singosaren	-7.8375931	110.3991398	jawa
node	2362739720	pertamina	Pertamina	SPBU	-7.1687214	107.8953417	jawa
node	2362775428	pertamina	SPBU 54-63206	SPBU 54-63206	-7.4029675	111.4125351	jawa
node	2362850257	pertamina	SPBU Kedungprahu	SPBU Kedungprahu	-7.4379166	111.5376253	jawa
node	2362871967	pertamina	Pertamina	SPBU Pertamina 54.632.01 Karang Asri	-7.4064625	111.4566441	jawa
node	2362874114	pertamina	Pertamina	SPBU Pertamina 54.632.04 Beran	-7.4166575	111.4420555	jawa
node	2370226523	pertamina	Pertamina	SPBU Pertamina 44.513.05 Jambearum	-6.9224277	110.178028	jawa
node	2384905199	pertamina	SPBU Gendingan	\\N	-7.3751972	111.2198749	jawa
node	2389187002	other	\\N	\\N	-7.4019129	109.3482326	jawa
node	2419456160	pertamina	Pertamina	Pertamina	-5.721921	105.5976415	sumatra
node	2420437945	other	\\N	\\N	-6.3392921	108.3377495	jawa
node	2476273184	pertamina	Pertamina	SPBU Pertamina 44.571.02 Pucangsawit	-7.5694769	110.8521464	jawa
node	2500443120	pertamina	Pertamina	Pertamina	-6.9492397	107.625816	jawa
node	2500443121	pertamina	Pertamina	Pertamina	-6.9548757	107.6117403	jawa
node	2500443125	pertamina	Pertamina	SPBU 34.413.45	-6.3554139	107.2298455	jawa
node	2509858851	pertamina	PT. Pertamina (Persero)	SPBU 34-46112	-7.6310915	108.1143098	jawa
node	2595420415	other	\\N	\\N	-7.2490332	110.4257274	jawa
node	2602421831	other	\\N	\\N	-7.3345238	110.0374264	jawa
node	2618740270	other	\\N	\\N	-7.2606542	109.0044619	jawa
node	2618965240	other	Sakalibel	Sakalibel	-7.2359208	109.0169601	jawa
node	2618965241	other	Negaradaha	Negaradaha	-7.2643191	109.0238056	jawa
node	2638405863	other	\\N	\\N	-7.2904398	110.4043954	jawa
node	2639371923	other	\\N	\\N	-6.3737153	106.9177516	jawa
node	2640576844	pertamina	SPBU Bojongkulur	SPBU Bojongkulur	-6.3194969	106.9661961	jawa
node	2641719661	other	\\N	\\N	-6.302018	106.9567005	jawa
node	2651041096	pertamina	Pertamina	SPBU Pertamina 44.557.09 Patalan	-7.9296865	110.34821	jawa
node	2657152189	pertamina	Pertamina	Pertamina	-7.3586282	110.2226436	jawa
node	2701279836	pertamina	Pertamina	Pertamina	-6.8910576	107.6256199	jawa
node	2746818873	pertamina	Pertamina	Pertamina	-6.9106838	107.7949608	jawa
node	2751350885	pertamina	SPBU MT Haryono	SPBU MT Haryono	-7.3138858	110.1644529	jawa
node	2751368734	pertamina	SPBU Buntu	SPBU Buntu	-7.5923135	109.2721123	jawa
node	2766239884	other	\\N	\\N	-7.0642005	108.0875996	jawa
node	2816560512	pertamina	Pertamina	Pertamina	-8.3438064	113.5947591	jawa
node	2881143282	pertamina	Pertamina	SPBU Pertamina 44.558.06 Sambipitu	-7.8770924	110.5382742	jawa
node	2896451690	pertamina	Pertamina	SPBU	-6.9199411	106.9190776	jawa
node	2896455730	pertamina	Pertamina	SPBU	-6.9177654	106.962804	jawa
node	2896455944	pertamina	Pertamina	SPBU	-6.9221403	106.94175	jawa
node	2898342403	pertamina	Pertamina	SPBU Pertamina 34.43107	-6.9345552	106.9319309	jawa
node	2924123252	pertamina	Pertamina	Pertamina	-6.8342437	107.4133042	jawa
node	2928627527	pertamina	Pertamina	Pertamina	-6.8686026	107.5289228	jawa
node	2932423768	pertamina	Pertamina	SPBU	-6.9022043	106.9867172	jawa
node	2932639219	pertamina	Pertamina	SPBU	-6.8269923	107.1362096	jawa
node	2941361682	pertamina	Pertamina	SPBU	-6.7846971	107.1751013	jawa
node	2968327804	other	\\N	\\N	-6.3962742	106.7782926	jawa
node	3011767690	pertamina	Pertamina	SPBU Sayangan	-7.3202721	110.5035676	jawa
node	3011767702	pertamina	Pertamina	SPBU	-7.3204565	110.4942945	jawa
node	3011810892	pertamina	SPBU Kemiri	SPBU Kemiri	-7.5478815	110.6092835	jawa
node	3017443032	pertamina	SPBU Joyoboyo	SPBU Joyoboyo	-7.8185461	112.019133	jawa
node	3026450327	pertamina	Pertamina	SPBU Osamaliki	-7.3299062	110.4960203	jawa
node	3038555799	pertamina	PT Pertamina	SPBU 43.502.01	-7.0029984	110.4441802	jawa
node	3050048021	other	\\N	\\N	-7.4833437	112.4275819	jawa
node	3052434975	pertamina	Pertamina	Pertamina	-6.2693046	107.0374774	jawa
node	3096058116	pertamina	Pertamina	Pertamina	-6.5684195	106.7382162	jawa
node	3104949404	pertamina	Pertamina	SPBU Pertamina 41.575.01 Solo Baru	-7.6005172	110.8136611	jawa
node	3171743973	pertamina	Pertamina	SPBU Pertamina	-6.4654856	106.8238397	jawa
node	3171743974	other	\\N	\\N	-6.4722364	106.8220826	jawa
node	3202925163	pertamina	SPBU Tambakberas	SPBU Tambakberas	-7.5213481	112.2329452	jawa
node	3226058383	pertamina	Pertamina	\\N	-7.5206991	112.5659934	jawa
node	3229039239	pertamina	Pertamina	SPBU 44.505.09	-7.1461453	110.4218471	jawa
node	3295094370	pertamina	Pertamina	Pertamina	-7.6247025	112.8643027	jawa
node	3297353361	pertamina	Pertamina	SPBU 54-61101	-7.1631136	112.6332702	jawa
node	3311113662	pertamina	Pertamina	Pertamina	-7.3309578	112.6324093	jawa
node	3316034004	pertamina	Pertamina	SPBU Pertamina 44.555.02 Helsen	-7.7085443	110.3547072	jawa
node	3318168724	pertamina	Pertamina	SPBU 54 - 63208	-7.3644106	111.1494057	jawa
node	3318188326	pertamina	Pertamina	SPBU Ongkrak 34.433.13	-6.8728385	106.7756381	jawa
node	3318188327	pertamina	Pertamina	SPBU 3443305	-6.8520944	106.7645311	jawa
node	3318601178	pertamina	Pertamina	Pertamina	-7.5456254	110.8072247	jawa
node	3320178541	pertamina	Pertamina	SPBU Pertamina 44.571.23 Bhayangkara	-7.5737207	110.8101785	jawa
node	3320401562	pertamina	Pertamina	SPBU Pertamina 44.555.02 Medari	-7.6765683	110.3367646	jawa
node	3320402063	pertamina	SPBU salam	SPBU salam	-7.6401898	110.322795	jawa
node	3390439062	pertamina	Pertamina	SPBU Pertamina 44.557.11 Pucung Pendowoharjo	-7.8528342	110.3419062	jawa
node	3409803927	pertamina	Pertamina	Pertamina	-6.2679799	107.0167532	jawa
node	3416884281	pertamina	Pertamina	SPBU 34-17516	-6.2650489	107.0302086	jawa
node	3426136852	pertamina	Pertamina	SPBU Pertamina 44.556.09 Temon	-7.8846274	110.0500754	jawa
node	3426583579	pertamina	Pertamina	Pertamina	-6.9045444	107.6734695	jawa
node	3426583583	pertamina	Pertamina	Pertamina	-6.9329256	107.7260125	jawa
node	3426583584	pertamina	Pertamina	Pertamina	-6.9387609	107.7340484	jawa
node	3426583585	pertamina	Pertamina	Pertamina	-6.9397477	107.7413424	jawa
node	3426585961	pertamina	Pertamina	Pertamina	-6.9289714	107.7120823	jawa
node	3428139820	pertamina	Pertamina	SPBU Pertamina 44.556.02 Sukoreno	-7.8717343	110.2082017	jawa
node	3428140378	pertamina	Pertamina	SPBU Pertamina 44.556.04 Ngramang	-7.8676612	110.180199	jawa
node	3429603357	pertamina	Pertamina	Pertamina	-6.9057833	107.6185972	jawa
node	3438532325	other	\\N	\\N	-8.1811228	111.117807	jawa
node	3438537224	other	\\N	\\N	-8.2044258	111.0972154	jawa
node	3438537477	other	\\N	\\N	-8.192589	111.1292121	jawa
node	3445891374	pertamina	SPBU secang	SPBU secang	-7.4021077	110.2408454	jawa
node	3445985781	pertamina	SPBU	SPBU	-7.5737963	110.2735523	jawa
node	3451608086	pertamina	Pertamina	SPBU Pertamina 44.557.05 Imogiri	-7.9150822	110.3820442	jawa
node	3458636578	other	\\N	\\N	-7.3644398	109.9177515	jawa
node	3458636580	other	POM Bensin Singkir	POM Bensin Singkir	-7.3758697	109.8995628	jawa
node	3463002183	pertamina	spbu	spbu	-6.9295871	109.6558469	jawa
node	3463005668	pertamina	SPBU	SPBU	-7.2259754	110.058686	jawa
node	3463005669	pertamina	SPBU	SPBU	-7.0892297	110.0482365	jawa
node	3463005671	pertamina	SPBU	SPBU	-6.903279	109.6638802	jawa
node	3463010178	pertamina	SPBU	SPBU	-7.4825279	110.2333959	jawa
node	3463010179	pertamina	Pertamina	Pertamina	-7.4608071	110.2229724	jawa
node	3463010409	pertamina	Pertamina	Pertamina	-7.5086505	110.2248769	jawa
node	3463010410	pertamina	SPBU	SPBU	-7.4880803	110.2345584	jawa
node	3463010484	pertamina	SPBU	SPBU	-7.3752524	110.220098	jawa
node	3463010487	pertamina	SPBU	SPBU	-7.1837443	110.0640694	jawa
node	3463012255	pertamina	Pertamina	Pertamina	-7.4335031	110.2287429	jawa
node	3466785797	pertamina	SPBU Pertamina 44.512.01 Petamanan	SPBU Pertamina 44.512.01 Petamanan	-6.9769154	109.9177855	jawa
node	3483020732	pertamina	PT Pertamina	Pertamina	-6.0944642	106.6898502	sumatra
node	3483651840	pertamina	SPBU Pertamina	SPBU Pertamina	-6.311208	106.9244007	jawa
node	3492622794	pertamina	PT Pertamina	SPBU Pertamina	-6.142002	106.6854052	sumatra
node	3492703914	pertamina	Pertamina	Pertamina	-7.384456	110.2380004	jawa
node	3553221096	pertamina	Pertamina	SPBU Pertamina 44.513.09 Jenarsari	-6.9541468	110.0965714	jawa
node	3587468004	pertamina	SPBU Terminal Cepu	SPBU Terminal Cepu	-7.154027	111.581601	jawa
node	3601877724	pertamina	Pertamina	SPBU Pertamina 44.574.04 Kebon Arum	-7.7187253	110.5773056	jawa
node	3616854458	pertamina	Pertamina	SPBU Pertamina 44.551.14 Tamantirto	-7.8189218	110.3245659	jawa
node	3621116437	pertamina	Pertamina	SPBU 34-17511	-6.2793186	107.0867893	jawa
node	3621116449	pertamina	Pertamina	Pertamina	-6.3231988	107.1272495	jawa
node	3621116454	pertamina	Pertamina	SPBU 34-41350	-6.3636677	107.2724176	jawa
node	3627922488	pertamina	Pertamina	SPBU	-6.5099589	107.7064153	jawa
node	3627931165	pertamina	Pertamina	SPBU	-6.3483283	107.2055078	jawa
node	3630751624	pertamina	Pertamina	SPBU Pertamina 44.551.03 Diro	-7.8609535	110.3405966	jawa
node	3640299019	pertamina	Pertamina	SPBU 34-17501	-6.3081632	107.1443556	jawa
node	3640299029	pertamina	Pertamina	SPBU	-6.2833104	107.2791657	jawa
node	3651126942	pertamina	Pertamina	Pom Bensin Lemah Abang	-7.176429	110.4183675	jawa
node	3659874036	other	\\N	\\N	-7.8971404	112.5555703	jawa
node	3659882527	other	\\N	\\N	-7.8645139	112.502071	jawa
node	3659882528	pertamina	Pertamina	Pertamina	-7.8681914	112.5185558	jawa
node	3659882529	other	\\N	\\N	-7.8753492	112.5311408	jawa
node	3662597828	pertamina	Pertamina	Pertamina	-7.5620674	111.8280158	jawa
node	3664023461	pertamina	Pertamina	Alam Sari	-6.8343867	107.9306983	jawa
node	3664052910	pertamina	Pertamina	Karapyak	-6.8350684	107.9171856	jawa
node	3664055625	pertamina	Pertamina	Barak	-6.8439669	107.9156597	jawa
node	3680076015	pertamina	Pertamina	SPBU Pertamina Cangkring	-6.8576161	110.7048777	jawa
node	3693646876	other	\\N	\\N	-7.3288674	110.1992652	jawa
node	3712962256	pertamina	Pertamina	SPBU Pertamina 54.661.08 Jl.Kenari	-8.1139211	112.1644686	jawa
node	3724099270	other	Pom Bensin Kedung Pucang	Pom Bensin Kedung Pucang	-7.6459416	110.0490513	jawa
node	3732365452	pertamina	Pertamina	SPBU	-6.2772734	107.0891216	jawa
node	3732368813	pertamina	PERTAMINA	SPBU	-6.3759985	107.0016759	jawa
node	3733958433	pertamina	Pertamina	SPBU	-6.4854654	106.8361109	jawa
node	3734052784	pertamina	PERTAMINA	SPBU Kincir	-6.475931	106.8036011	jawa
node	3740385556	pertamina	Pertamina 34-17542	Pertamina 34-17542	-6.2133891	107.0337856	jawa
node	3758480424	other	\\N	\\N	-6.175008	106.5988294	sumatra
node	3761445487	pertamina	Pertamina	SPBU Pertamina 54.614.17	-7.5906237	112.1321511	jawa
node	3775318734	other	Pom Bensin 56.683.11	Pom Bensin 56.683.11	-7.7592714	114.2635867	jawa
node	3840368130	pertamina	Pertamina	SPBU Pertamina 54.661.01 Poluan	-8.0652087	112.0949093	jawa
node	3887175271	pertamina	Pertamina	\\N	-6.6403074	106.7853419	jawa
node	3926522548	other	Pom Bensin	Pom Bensin	-6.9058108	109.7221174	jawa
node	4026943236	pertamina	Pertamina	SPBU Pertamina 44.556.05 Kenteng Nanggulan	-7.7562954	110.2111182	jawa
node	4028010720	pertamina	SPBU Pertamina	SPBU Pertamina	-6.277896	107.8503847	jawa
node	4076139413	pertamina	Pertamina	SPBU Pertamina 44.555.07 Mangunan	-7.6610396	110.4071484	jawa
node	4109592321	other	\\N	\\N	-6.7665907	111.7094703	jawa
node	4126300393	pertamina	spbu muri	spbu muri	-6.8613783	109.16483	jawa
node	4143020532	pertamina	Pertamina	Pertamina	-6.9223889	107.6440384	jawa
node	4143041693	pertamina	Pertamina	Pertamina	-6.9136217	107.6517728	jawa
node	4143047589	pertamina	Pertamina	Pertamina	-6.9183776	107.644464	jawa
node	4143049089	pertamina	Pertamina	Pertamina	-6.9136483	107.6589018	jawa
node	4185448861	pertamina	Pertamina	SPBU Pertamina 44.557.03 Cengkiran	-7.9140774	110.2864736	jawa
node	4185448862	pertamina	Pertamina	SPBU Pertamina 44.551.18 Sewon	-7.8447923	110.3610179	jawa
node	4192745643	pertamina	Pertamina	Pertamina	-7.3736424	112.7591333	jawa
node	4198510245	other	gas station	gas station	-8.2045488	114.3185836	jawa
node	4218409991	other	Pangkalan Ojeg Cilame	Pangkalan Ojeg Cilame	-7.2004466	107.842385	jawa
node	4223410331	pertamina	SPBU Pedotan	SPBU Pedotan	-8.485707	114.1375948	jawa
node	4227282492	other	Pom Bensin Cikembang	Pom Bensin Cikembang	-6.9500973	106.7709471	jawa
node	4227305189	other	Pom Bensin Ciawi	Pom Bensin Ciawi	-6.6597768	106.845504	jawa
node	4230893603	pertamina	SPBU Cikembang to Ubrug	SPBU Cikembang to Ubrug	-6.9534398	106.7680292	jawa
node	4230925001	pertamina	SPBU Cikembang Armed	SPBU Cikembang Armed	-6.9532514	106.7682542	jawa
node	4266129189	other	Nissan	Nissan	-6.75598	111.69308	jawa
node	4271048933	pertamina	SPBU	\\N	-6.3389948	106.6725536	jawa
node	4304119089	pertamina	SPBU Procot	SPBU Procot	-6.9629624	109.1386136	jawa
node	4307387590	pertamina	SPBU Kasembon	SPBU Kasembon	-7.7819807	112.3104067	jawa
node	4327908913	pertamina	Pertamina	Pertamina	-6.9129281	107.5746074	jawa
node	4329578164	pertamina	Pertamina	Pertamina	-6.8992841	107.6419203	jawa
node	4360820589	pertamina	SPBU Pertamina Jetak	SPBU Pertamina Jetak	-7.1634894	111.8692219	jawa
node	4360848290	pertamina	Pertamina	SPBU Pertamina	-7.1357197	111.9220234	jawa
node	4374920043	pertamina	Pertamina	Pertamina	-7.3003049	112.6621415	jawa
node	4379249263	other	\\N	\\N	-7.5541269	112.233393	jawa
node	4400759110	pertamina	SPBU Arjosari	SPBU Arjosari	-8.1244677	111.1474755	jawa
node	4411033750	other	\\N	\\N	-7.5512216	112.2533076	jawa
node	4443241491	pertamina	SPBU	SPBU	-6.3181195	106.7893456	jawa
node	4444950889	pertamina	Pertamina	SPBU 53-62322	-6.8296138	111.9962518	jawa
node	4467668251	pertamina	Pertamina	SPBU Pertamina	-6.7123924	108.5320862	jawa
node	4467738625	pertamina	Pertamina	Pertamina	-8.1772879	114.7500647	jawa
node	4472320636	pertamina	Pertamina	Pertamina	-7.2903351	112.7842866	jawa
node	4472619669	pertamina	Pertamina	SPBU Pertamina	-6.7166823	108.5510332	jawa
node	4472637369	pertamina	Pertamina	SPBU Pertamina	-6.7270487	108.5500385	jawa
node	4476282609	pertamina	Pertamina	Pertamina	-7.3009602	112.7813011	jawa
node	4549223696	pertamina	Pertamina	Pertamina	-7.1825976	112.6138973	jawa
node	4556364806	pertamina	GROUP NAWAWI	SPBU Kebonagung	-7.7484311	113.4486884	jawa
node	4564829814	pertamina	Pertamina	Pertamina	-7.2846901	112.7339126	jawa
node	4565611189	pertamina	Pertamina	Pertamina	-7.3427458	112.7550449	jawa
node	4583496096	other	\\N	\\N	-7.2280229	110.3467521	jawa
node	4584682591	pertamina	Pertamina	Pertamina	-7.5824253	112.6919622	jawa
node	4584682592	pertamina	Pertamina	Pertamina	-7.5931918	112.6901541	jawa
node	4584740191	pertamina	Pertamina	Pertamina	-7.611849	112.6871887	jawa
node	4584748190	pertamina	Pertamina	Pertamina	-7.5576641	112.6950241	jawa
node	4584761801	pertamina	Pertamina	Pertamina	-7.665521	112.6692226	jawa
node	4585212029	pertamina	Pertamina	Pertamina	-7.4265264	112.6933344	jawa
node	4585212224	pertamina	Pertamina	Pertamina	-7.5645782	112.6768585	jawa
node	4587011090	pertamina	SPBU 34-15322	SPBU 34-15322	-6.3438707	106.6872034	jawa
node	4588990878	pertamina	Pertamina	SPBU Pertamina 54.671.23 Raci	-7.6077771	112.8296044	jawa
node	4588992081	akr	AKR	AKR	-7.7168714	112.8995229	jawa
node	4588992283	pertamina	Pertamina	Pertamina	-7.5937937	112.7625986	jawa
node	4588992439	pertamina	Pertamina	Pertamina	-7.6052738	112.80013	jawa
node	4588992494	pertamina	Pertamina	Pertamina	-7.6888044	112.8898478	jawa
node	4588992694	pertamina	Pertamina	Pertamina	-7.60956	112.8356367	jawa
node	4591958176	pertamina	Pertamina	SPBU Pertamina	-6.7012839	108.4808315	jawa
node	4620143189	pertamina	Pertamina	SPBU 54-63212	-7.3953572	111.3675672	jawa
node	4624678903	pertamina	Pertamina	Pertamina	-7.2229467	112.7318824	jawa
node	4664656218	pertamina	SPBU	SPBU	-7.4139564	112.7252118	jawa
node	4664659131	pertamina	SPBU Pertamina 54.612.36 - Jenggolo	SPBU Pertamina 54.612.36 - Jenggolo	-7.4410557	112.7201555	jawa
node	4664659138	pertamina	SPBU	SPBU	-7.4428293	112.7178205	jawa
node	4670675145	pertamina	Pertamina	SPBU Pertamina 54.632.03 Margomulyo	-7.4083708	111.437796	jawa
node	4673984275	pertamina	SPBU 54-61254	SPBU 54-61254	-7.3971118	112.6982986	jawa
node	4674022371	pertamina	SPBU 54-61224	SPBU 54-61224	-7.4443718	112.6916137	jawa
node	4674046961	pertamina	SPBU 54-61244	SPBU 54-61244	-7.4493027	112.7085767	jawa
node	4677113148	pertamina	SPBU 54-63301	SPBU 54-63301	-7.646589	111.3473505	jawa
node	4677517349	pertamina	SPBU-5463117	SPBU-5463117	-7.6063428	111.534459	jawa
node	4679407322	pertamina	SPBU 54-63211	SPBU 54-63211	-7.5145505	111.2636454	jawa
node	4679419943	pertamina	SPBU 54-63218	SPBU 54-63218	-7.4460053	111.3786478	jawa
node	4681507834	pertamina	SPBU 34 - 41216	SPBU 34 - 41216	-6.655829	107.7157404	jawa
node	4681508061	pertamina	SPBU 34 - 41228	SPBU 34 - 41228	-6.5818404	107.7451499	jawa
node	4681508062	pertamina	SPBU 34 - 41205	SPBU 34 - 41205	-6.5816576	107.7463108	jawa
node	4682458663	pertamina	SPBU 34 - 41207	SPBU 34 - 41207	-6.5467753	107.7765817	jawa
node	4684795671	pertamina	Pertamina	SPBU	-7.2180105	107.892952	jawa
node	4686720820	pertamina	SPBU 54-61260	SPBU 54-61260	-7.4606528	112.723187	jawa
node	4701450633	other	\\N	\\N	-6.8397113	110.874282	jawa
node	4737981691	pertamina	Pertamina	Pom Bensin 54.683.10	-7.7243283	113.6205125	jawa
node	4752447199	other	\\N	\\N	-7.758784	113.1758771	jawa
node	4774253710	pertamina	Pertamina	SPBU 44.511.04	-6.8886829	109.6659712	jawa
node	4785469489	pertamina	Pertamina	Pertamina	-7.298784	112.7608231	jawa
node	4785469490	pertamina	Pertamina	Pertamina	-7.3365777	112.7031288	jawa
node	4785469491	pertamina	Pertamina	Pertamina	-7.2864174	112.7756953	jawa
node	4785469492	pertamina	Pertamina	Pertamina	-7.296664	112.7424338	jawa
node	4785469493	pertamina	Pertamina	Pertamina	-7.265884	112.7064562	jawa
node	4785469494	pertamina	Pertamina	Pertamina	-7.2588552	112.6790877	jawa
node	4785469495	pertamina	Pertamina	Pertamina	-7.2508829	112.7299569	jawa
node	4785469496	pertamina	Pertamina	Pertamina	-7.2785735	112.7555984	jawa
node	4785469497	pertamina	Pertamina	Pertamina	-7.3181922	112.7514758	jawa
node	4785469498	pertamina	Pertamina	Pertamina	-7.2852487	112.6975852	jawa
node	4785469499	pertamina	Pertamina	Pertamina	-7.2790587	112.7295461	jawa
node	4785469500	pertamina	Pertamina	Pertamina	-7.2848287	112.6755814	jawa
node	4785469501	pertamina	Pertamina	Pertamina	-7.3200616	112.7465797	jawa
node	4785469504	pertamina	Pertamina	Pertamina	-7.3132092	112.6984354	jawa
node	4785469505	pertamina	Pertamina	Pertamina	-7.3389412	112.699429	jawa
node	4785469506	pertamina	Pertamina	Pertamina	-7.3068905	112.6637779	jawa
node	4785469508	pertamina	Pertamina	Pertamina	-7.2446315	112.7163151	jawa
node	4785469509	pertamina	Pertamina	Pertamina	-7.3325564	112.6918097	jawa
node	4785469510	pertamina	Pertamina	Pertamina	-7.2712839	112.7967306	jawa
node	4785469511	pertamina	Pertamina	Pertamina	-7.2660257	112.7378505	jawa
node	4785469512	pertamina	Pertamina	Pertamina	-7.2319976	112.7738302	jawa
node	4785469513	pertamina	Pertamina	Pertamina	-7.2455575	112.7293916	jawa
node	4785469514	pertamina	Pertamina	Pertamina	-7.2756816	112.7073553	jawa
node	4785469515	pertamina	Pertamina	Pertamina	-7.2639925	112.6874883	jawa
node	4785469516	pertamina	Pertamina	Pertamina	-7.2561257	112.7465183	jawa
node	4785469517	pertamina	Pertamina	Pertamina	-7.2783343	112.8076463	jawa
node	4785469518	pertamina	Pertamina	Pertamina	-7.2525433	112.7352128	jawa
node	4785469519	pertamina	Pertamina	Pertamina	-7.2683736	112.7510288	jawa
node	4785469520	pertamina	Pertamina	Pertamina	-7.3305505	112.7570858	jawa
node	4785469821	pertamina	Pertamina	Pertamina	-7.311253	112.7240447	jawa
node	4785469822	pertamina	Pertamina	Pertamina	-7.2603911	112.6934832	jawa
node	4785469823	pertamina	Pertamina	Pertamina	-7.2668848	112.7684264	jawa
node	4785469824	pertamina	Pertamina	Pertamina	-7.2918203	112.7233371	jawa
node	4785469829	pertamina	Pertamina	Pertamina	-7.2698378	112.7463785	jawa
node	4785469830	pertamina	Pertamina	Pertamina	-7.327859	112.7110126	jawa
node	4785469831	pertamina	Pertamina	Pertamina	-7.2896942	112.7105449	jawa
node	4785469832	pertamina	Pertamina	Pertamina	-7.3063757	112.7601073	jawa
node	4785469833	pertamina	Pertamina	Pertamina	-7.2992263	112.731701	jawa
node	4785469834	pertamina	Pertamina	Pertamina	-7.2578204	112.680948	jawa
node	4785469835	pertamina	Pertamina	Pertamina	-7.2493313	112.7105983	jawa
node	4785469836	pertamina	Pertamina	Pertamina	-7.322002	112.7853771	jawa
node	4785469837	pertamina	Pertamina	Pertamina	-7.2543663	112.7497935	jawa
node	4785469838	pertamina	Pertamina	Pertamina	-7.2483761	112.7212043	jawa
node	4785469839	pertamina	Pertamina	Pertamina	-7.3348821	112.7038955	jawa
node	4785469840	pertamina	Pertamina	Pertamina	-7.2551937	112.6626909	jawa
node	4785469841	pertamina	Pertamina	Pertamina	-7.2808034	112.7717369	jawa
node	4785469842	pertamina	Pertamina	Pertamina	-7.2999606	112.761684	jawa
node	4785469843	pertamina	Pertamina	Pertamina	-7.3215231	112.769943	jawa
node	4785469844	pertamina	Pertamina	Pertamina	-7.3184096	112.7455397	jawa
node	4785469849	pertamina	Pertamina	Pertamina	-7.2771743	112.7462194	jawa
node	4785469850	pertamina	Pertamina	Pertamina	-7.2580162	112.7826183	jawa
node	4785469851	pertamina	Pertamina	Pertamina	-7.2487994	112.7548521	jawa
node	4785469852	pertamina	Pertamina	Pertamina	-7.2429779	112.7592409	jawa
node	4785469853	pertamina	Pertamina	Pertamina	-7.2417746	112.7573405	jawa
node	4785469854	pertamina	Pertamina	Pertamina	-7.2228744	112.7365158	jawa
node	4785469855	pertamina	Pertamina	Pertamina	-7.2352675	112.735229	jawa
node	4785469856	pertamina	Pertamina	Pertamina	-7.2311066	112.7259003	jawa
node	4785469857	pertamina	Pertamina	Pertamina	-7.2291309	112.7086617	jawa
node	4785469858	pertamina	Pertamina	Pertamina	-7.2293485	112.6764578	jawa
node	4785469859	pertamina	Pertamina	Pertamina	-7.2138415	112.6530327	jawa
node	4785672526	pertamina	Pertamina	Pertamina	-6.4764217	106.8462731	jawa
node	4795153365	pertamina	Pertamina	Pertamina	-7.2250837	112.7420332	jawa
node	4795153366	pertamina	Pertamina	Pertamina	-7.306757	112.7228567	jawa
node	4795165703	pertamina	Pertamina	Pertamina	-7.3086169	112.7678717	jawa
node	4795168079	pertamina	Pertamina	Pertamina	-7.2451436	112.747946	jawa
node	4795168080	pertamina	Pertamina	SPBU Pertamina Stasiun Kota 5460144	-7.2425279	112.7397892	jawa
node	4795168081	pertamina	Pertamina	Pertamina	-7.2863779	112.7455029	jawa
node	4795168621	pertamina	Pertamina	Pertamina	-7.2331829	112.7537037	jawa
node	4795172424	pertamina	Pertamina	SPBU Pertamina Ploso Baru Sidoarjo	-7.2585852	112.7738642	jawa
node	4795172425	pertamina	Pertamina	Pertamina	-7.249716	112.7371487	jawa
node	4795172653	pertamina	Pertamina	Pertamina	-7.2500331	112.7647314	jawa
node	4795172655	pertamina	Pertamina	Pertamina	-7.2843649	112.6911929	jawa
node	4795173765	pertamina	Pertamina	Pertamina	-7.2888059	112.7583468	jawa
node	4795173766	pertamina	Pertamina	Pertamina	-7.2577494	112.7276159	jawa
node	4795173767	pertamina	Pertamina	Pertamina	-7.2630839	112.7262484	jawa
node	4795187915	pertamina	Pertamina	Pertamina	-7.3346773	112.7749754	jawa
node	4795298321	pertamina	Pertamina	SPBU Pertamina 44.555.13 Banyuraden	-7.7798275	110.3431886	jawa
node	4799118025	pertamina	Pertamina	SPBU Pertamina 44.552.16 Munggur	-7.7855758	110.3879231	jawa
node	4818670121	other	\\N	\\N	-6.4310901	106.965542	jawa
node	4832316137	pertamina	PT Pertamina	31.127.01	-6.2735191	106.8188158	jawa
node	4840773555	pertamina	PT Pertamina	SPBU 34.13413	-6.2218999	106.8862411	jawa
node	4841133538	pertamina	Pertamina	Pertamina	-6.2352932	106.8778219	jawa
node	4841684309	pertamina	Pertamina	Pertamina	-6.1854894	106.9480772	sumatra
node	4841684319	pertamina	PT Pertamina	SPBU 10048251	-6.1843023	106.9445459	sumatra
node	4849205567	pertamina	Pertamina	Pertamina	-6.1970882	106.8139672	sumatra
node	4849427752	pertamina	Pertamina	SPBU Pertamina 34-10504	-6.1848315	106.8750376	sumatra
node	4855236364	pertamina	PT Pertamina	SPBU 34.11405	-6.2036587	106.7970228	jawa
node	4855915799	shell	Shell	Shell	-6.1845383	106.7365457	sumatra
node	4859352224	pertamina	PT Pertamina	SPBU 34 11602	-6.1789556	106.7486143	sumatra
node	4859352278	pertamina	PT Pertamina	SPBU 34-11611	-6.1868261	106.7253569	sumatra
node	4863790792	pertamina	PT Pertamina	SPBU 34.112.01	-6.1555621	106.8071193	sumatra
node	4864234327	pertamina	Pertamina	Pertamina	-6.1972387	106.7517865	sumatra
node	4871574186	pertamina	PT Pertamina	SPBU 34-11606	-6.1982216	106.7602243	sumatra
node	4877095390	pertamina	Pertamina	SPBU 34-13908	-6.1895404	106.9673268	sumatra
node	4882725668	pertamina	PT Pertamina	SPBU 34 11607	-6.1960559	106.7298389	sumatra
node	4883234773	pertamina	PT Pertamina	31.102.02	-6.1785704	106.8181908	sumatra
node	4883234775	pertamina	Pertamina	34-10205	-6.1803988	106.8136199	sumatra
node	4884738032	pertamina	Pertamina	Pertamina	-6.1930955	106.8997907	sumatra
node	4884984709	pertamina	Pertamina	Pertamina	-6.1966305	106.8748459	sumatra
node	4887139094	pertamina	Pertamina	Pertamina	-6.2338285	106.8761993	jawa
node	4887170808	pertamina	PT Pertamina	SPBU 34-10604	-6.1677556	106.8426633	sumatra
node	4892485177	pertamina	PT Pertamina	SPBU 34-13907	-6.2078268	106.9539593	jawa
node	4892485669	pertamina	PT Pertamina	SPBU 34-13905	-6.1997376	106.9559518	sumatra
node	4903253111	pertamina	PT Pertamina	SPBU 34-13709	-6.3407968	106.8748711	jawa
node	4910065573	pertamina	PT Pertamina	SPBU 34-11609	-6.2191008	106.7309374	jawa
node	4910182664	pertamina	PT Pertamina	SPBU 3412601	-6.3428676	106.8179439	jawa
node	4910293116	pertamina	SPBU	SPBU	-6.1976979	106.753381	sumatra
node	4913949474	pertamina	PT Pertamina	SPBU 34 11604	-6.2189393	106.7604913	jawa
node	4913949515	pertamina	Pertamina	Pertamina	-6.2173356	106.763519	jawa
node	4916416966	pertamina	PT Pertamina	SPBU Pertamina 31.118-02	-6.1551928	106.7112324	sumatra
node	4916416974	pertamina	PT Pertamina	SPBU Pertamina 31.B.114.02	-6.1551074	106.7117393	sumatra
node	4918138365	pertamina	PT Pertamina	SPBT Kemhan	-6.2427738	106.8779252	jawa
node	4918263355	pertamina	PT Pertamina	34 - 11509	-6.2127071	106.7724768	jawa
node	4918290669	pertamina	PT Pertamina	SPBU Pertamina 31-118-03	-6.1589708	106.6928621	sumatra
node	4925980474	pertamina	PT Pertamina	SPBG 31.A.124.02	-6.2957076	106.7723304	jawa
node	4928014336	pertamina	Pertamina	Pertamina	-6.3005094	106.8405986	jawa
node	4928121810	pertamina	PT Pertamina	34 12410	-6.3040855	106.7820615	jawa
node	4928317770	pertamina	Pertamina	Pertamina	-6.1146753	106.6889191	sumatra
node	4939483122	pertamina	Pertamina	SPBU Pertamina 44.551.05 Gambiran	-7.8181482	110.3903283	jawa
node	4945135424	pertamina	Pertamina	Pertamina	-7.7673272	113.1731054	jawa
node	4956471291	pertamina	PT Pertamina	SPBU 34-13306	-6.2197321	106.8677111	jawa
node	4960896352	pertamina	PT Pertamina	SPBU 34-11403	-6.190142	106.7931524	sumatra
node	4960896353	pertamina	PT Pertamina	SPBU 34.114.07	-6.188938	106.7879838	sumatra
node	4963491597	pertamina	PT Pertamina	SPBU 34-13421	-6.2183322	106.9425823	jawa
node	4965326324	pertamina	PT Pertamina	SPBU 3412606	-6.333822	106.8065882	jawa
node	4965837975	other	\\N	\\N	-8.1824446	114.4387078	jawa
node	4967867344	pertamina	Pertamina	Pertamina	-7.4952641	107.3836729	jawa
node	4968034605	pertamina	Pertamina	Pertamina	-6.2740575	106.8465351	jawa
node	4968460032	pertamina	Pertamina	Pertamina	-7.3606435	110.5137286	jawa
node	4969771444	pertamina	PT Pertamina	SPBU Pertamina 34-11806	-6.1303994	106.7033172	sumatra
node	4970512841	pertamina	Pertamina	Pertamina	-7.535232	110.6681385	jawa
node	4974492290	pertamina	PT Pertamina	34.11511	-6.1950484	106.7819424	sumatra
node	4974594507	pertamina	Pertamina	Pertamina	-6.1430555	106.6982886	sumatra
node	4974780407	pertamina	Pertamina	Pertamina	-6.1962238	106.769136	sumatra
node	4974780412	pertamina	PT Pertamina	34 11508	-6.2005263	106.7693039	jawa
node	4974780413	pertamina	PT Pertamina	34 11505	-6.1978948	106.7716258	sumatra
node	4976384112	pertamina	PT Pertamina	SPBU 34.13706	-6.3660687	106.8885465	jawa
node	4976447273	pertamina	Pertamina	Pertamina	-6.3288827	106.8861647	jawa
node	4978664990	pertamina	PT Pertamina	34.11507	-6.2086047	106.7701343	jawa
node	4978664991	shell	Shell	Shell	-6.2092455	106.7708911	jawa
node	4978679985	pertamina	PT Pertamina	34.124.11	-6.2634862	106.7866507	jawa
node	4978806205	pertamina	Pertamina	\\N	-6.6365525	107.3958903	jawa
node	4978825621	other	\\N	\\N	-6.6253584	107.4043587	jawa
node	4979266363	pertamina	PT Pertamina	34.11506	-6.1772565	106.7575308	sumatra
node	4979266364	pertamina	PT Pertamina	34.11510	-6.180008	106.7664233	sumatra
node	4981828257	pertamina	PT Pertamina	34-12409	-6.30926	106.7807406	jawa
node	4981849392	pertamina	PT Pertamina	Pertamina 34-13808	-6.3319619	106.8888744	jawa
node	4982237083	pertamina	PT Pertamina	SPBU 34 - 12608	-6.3289186	106.8253952	jawa
node	4982237090	pertamina	PT Pertamina	SPBU 34 - 12609	-6.3134417	106.812471	jawa
node	4982237091	pertamina	PT Pertamina	SPBU 34 - 12508	-6.3269965	106.8226555	jawa
node	4982521401	pertamina	PT Pertamina	SPBU Pertamina 34-11802	-6.1036527	106.7109293	sumatra
node	4984102643	pertamina	PT Pertamina	34.11504	-6.1889114	106.7619954	sumatra
node	4984663538	pertamina	PT Pertamina	SPBU 34.16924	-6.353416	106.8602613	jawa
node	4986721327	pertamina	Pertamina	Pertamina	-7.3945965	110.5135593	jawa
node	4986721333	pertamina	Pertamina	Pertamina	-7.3982651	110.5138697	jawa
node	4991460917	pertamina	PT Pertamina	SPBU	-6.3400085	106.8909001	jawa
node	4999291232	pertamina	Pertamina	Pertamina	-6.296287	106.8152293	jawa
node	4999344621	pertamina	Pertamina	Pertamina	-6.2473591	106.9113892	jawa
node	4999344682	pertamina	Pertamina	Pertamina	-6.2257652	106.9231197	jawa
node	4999344688	pertamina	Pertamina	Pertamina	-6.2255124	106.9235654	jawa
node	4999597234	pertamina	PT Pertamina	SPBU 134 126 10	-6.3535677	106.8107908	jawa
node	4999597238	pertamina	Pertamina	SPBU 34 126 11	-6.3498905	106.8021083	jawa
node	5006207260	shell	Shell	Shell	-6.1890471	106.7716424	sumatra
node	5006351548	shell	Shell	Shell	-6.1635128	106.7743767	sumatra
node	5006352161	pertamina	PT Pertamina	34.11704	-6.164141	106.7765603	sumatra
node	5006573807	pertamina	PT Pertamina	SPBU 34-13704	-6.3209883	106.8654373	jawa
node	5006840493	pertamina	PT Pertamina	34-12407	-6.2822186	106.7914505	jawa
node	5006840494	total	Total	Total	-6.2838156	106.7901349	jawa
node	5009129454	shell	Shell	Shell	-6.1688464	106.7613924	sumatra
node	5013168986	pertamina	PT Pertamina	34.11502	-6.1654516	106.7635059	sumatra
node	5013275447	pertamina	Pertamina	Pertamina	-6.1692277	106.7284012	sumatra
node	5014048533	pertamina	Pertamina	Pertamina	-6.1607369	106.632764	sumatra
node	5019465646	pertamina	PT Pertamina	SPBU 34 - 12605	-6.3122487	106.8367946	jawa
node	5019465649	pertamina	Pertamina	Pertamina	-6.3182969	106.8365812	jawa
node	5019465651	pertamina	PT Pertamina	SPBU 34 - 12603	-6.3208332	106.8371236	jawa
node	5023353974	pertamina	PT Pertamina	SPBU 34-14304	-6.1109176	106.8835214	sumatra
node	5023353975	pertamina	PT Pertamina	SPBU 34-14308	-6.1101948	106.8849778	sumatra
node	5024153697	pertamina	PT Pertamina	SPBG PT Jakarta Utilitas Propertindo	-6.3114292	106.8840168	jawa
node	5027538280	pertamina	PT Pertamina	34.12204	-6.2286926	106.7781397	jawa
node	5033751702	pertamina	PT Pertamina	SPBU 34 11708	-6.1827102	106.7188311	sumatra
node	5034144483	pertamina	Pertamina	Pertamina	-6.1692917	106.7272639	sumatra
node	5035130026	pertamina	Pom Bensin Pertamina Setu	Pom Bensin Pertamina Setu	-6.3193044	107.0275207	jawa
node	5042318788	pertamina	PT Pertamina	SPBU 34-11706	-6.1525873	106.7193246	sumatra
node	5042694087	pertamina	PT Pertamina	Pertamina 34-13803	-6.3165288	106.8918751	jawa
node	5042931433	pertamina	Pertamina	Pertamina	-6.1304848	106.7295384	sumatra
node	5047254145	pertamina	PT Pertamina	SPBU 34-11711	-6.144627	106.7213014	sumatra
node	5047494889	pertamina	Pertamina	Pertamina	-6.1662019	106.7871248	sumatra
node	5053533227	pertamina	Pertamina	Pertamina	-6.2594816	106.7820883	jawa
node	5055779301	pertamina	PT Pertamina	SPBU 34-13708	-6.3451135	106.8830681	jawa
node	5055940525	pertamina	PT Pertamina	SPBU 34-11712	-6.1546087	106.7449504	sumatra
node	5055940546	pertamina	Pertamina	Pertamina	-6.1488325	106.7282724	sumatra
node	5056125428	pertamina	Pertamina	34.11402	-6.1742623	106.8032455	sumatra
node	5056426528	pertamina	SPBU	SPBU	-6.6269699	106.7797498	jawa
node	5057618587	pertamina	Pertamina	Pertamina	-6.133877	106.730783	sumatra
node	5057662821	pertamina	Pertamina	Pertamina	-6.8280243	107.3758964	jawa
node	5057790969	pertamina	PT Pertamina	34.12213	-6.2599348	106.7768526	jawa
node	5057790970	pertamina	Pertamina	Pertamina	-6.2573118	106.7813531	jawa
node	5057790971	pertamina	Pertamina	Pertamina	-6.2532839	106.7731914	jawa
node	5063666171	pertamina	Pertamina	Pertamina	-6.2581645	106.7663544	jawa
node	5063666176	pertamina	Pertamina	Pertamina	-6.2700637	106.765078	jawa
node	5063666199	pertamina	PT Pertamina	SPBU Pertamina 3412306	-6.2705763	106.7599908	jawa
node	5068495577	other	Mako Kolinlamil 32-14301	Mako Kolinlamil 32-14301	-6.1655463	106.8898544	sumatra
node	5068912660	shell	Shell	Shell	-6.1560301	106.7553302	sumatra
node	5068912664	pertamina	PT Pertamina	SPBU 34.117.13	-6.158296	106.7659996	sumatra
node	5078742327	other	Pomp Bensin Jatibarang	Pomp Bensin Jatibarang	-6.4660078	108.2995081	jawa
node	5080659280	pertamina	PT Pertamina	SPBT 32.1.0401	-6.1657956	106.8897964	sumatra
node	5081112308	pertamina	Pertamina	Pertamina 34 - 14201	-6.1584075	106.9014895	sumatra
node	5082958545	pertamina	PT Pertamina	34.11205	-6.2080987	106.7880964	jawa
node	5083178709	pertamina	PT Pertamina	SPBU Pertamina 34. 12115	-6.2595467	106.7888648	jawa
node	5084987226	pertamina	Pertamina	Pertamina	-6.1452184	106.7764778	sumatra
node	5104583945	pertamina	PT Pertamina	SPBB	-6.1041519	106.7764009	sumatra
node	5104583952	pertamina	PT Pertamina	SPBU 34 - 14406	-6.10679	106.7750962	sumatra
node	5104583966	pertamina	PT Pertamina	\\N	-6.1027699	106.7719388	sumatra
node	5104675988	pertamina	PT Pertamina	34.14409	-6.1072345	106.7198342	sumatra
node	5104675989	pertamina	PT Pertamina	34.14410	-6.1340918	106.7511906	sumatra
node	5104675990	pertamina	PT Pertamina	34 14411	-6.1216094	106.7324996	sumatra
node	5104981269	pertamina	Pertamina	Pertamina	-6.1160305	106.7853625	sumatra
node	5104981276	pertamina	PT Pertamina	SPBG	-6.1182801	106.7918758	sumatra
node	5106742602	pertamina	PT Pertamina	SPBKB 10.2.1.001	-6.1399265	106.7673425	sumatra
node	5106742606	pertamina	PT Pertamina	SPBU Pertamina 34.14404	-6.1387449	106.7635609	sumatra
node	5106761379	pertamina	Pertamina	Pertamina	-7.8773154	113.0535746	jawa
node	5108763054	pertamina	Pertamina	Pertamina	-6.9266538	107.5748775	jawa
node	5112245187	pertamina	PT Pertamina	SPBU 34 - 14412	-6.1072087	106.8006862	sumatra
node	5112245651	pertamina	PT Pertamina	SPBU 3414401	-6.1257509	106.8017471	sumatra
node	5112284592	pertamina	Pertamina	Pertamina 34-14205	-6.1667583	106.9152278	sumatra
node	5114085557	pertamina	PT Pertamina	SPBU 34-14105	-6.1324478	106.9339608	sumatra
node	5128190822	pertamina	PT Pertamina	SPBU 34.14107	-6.1383793	106.9356633	sumatra
node	5130476888	pertamina	PT Pertamina	SPBU Pertamina 34.14106	-6.1073145	106.9625902	sumatra
node	5132941190	pertamina	Pertamina	Pertamina 34 - 14109	-6.1029236	106.9372407	sumatra
node	5134336081	pertamina	PT Pertamina	SBPU 31-143-01	-6.145331	106.8895483	sumatra
node	5134336082	pertamina	Pertamina	Pertamina	-6.1529554	106.8864182	sumatra
node	5141381721	pertamina	Pertamina	Pertamina	-6.8880776	107.5557111	jawa
node	5142536031	pertamina	PT Pertamina	SPBU 34-143-03	-6.1308308	106.8297578	sumatra
node	5142536041	pertamina	PT Pertamina	SPBU 34-14405	-6.1337917	106.8171716	sumatra
node	5158433612	pertamina	PT Pertamina	SPBU 34-13905	-6.1998189	106.955874	sumatra
node	5160138738	pertamina	PT Pertamina	SPBU 34-12705	-6.2438113	106.8527997	jawa
node	5160138741	pertamina	PT Pertamina	SPBU 34-14108	-6.1110269	106.9482857	sumatra
node	5177959021	pertamina	Pertamina	Pertamina	-6.9138016	107.6951878	jawa
node	5219389661	pertamina	SPBU 3415402	SPBU 3415402	-6.3179587	106.7691779	jawa
node	5225836450	other	33.154.01	33.154.01	-6.3054025	106.7730117	jawa
node	5225836451	other	34-15404	34-15404	-6.3107984	106.7712307	jawa
node	5226103536	pertamina	Pertamina	Pertamina	-6.3025872	106.760931	jawa
node	5230303581	other	\\N	\\N	-7.2026503	110.4235746	jawa
node	5243797921	pertamina	Pertamina	Pertamina	-6.6823566	105.9729892	jawa
node	5245226521	pertamina	Pertamina	Pertamina	-6.9543418	106.4801037	jawa
node	5263243856	pertamina	SPBU	SPBU	-7.4009355	109.8892315	jawa
node	5268872144	pertamina	Pertamina	Pertamina	-6.9522858	107.6379847	jawa
node	5291286821	pertamina	Pertamina	Pertamina	-8.129303	112.5503364	jawa
node	5296700734	other	\\N	\\N	-6.3002305	107.7504811	jawa
node	5302792621	other	Oil gas	Oil gas	-6.2460658	106.9058632	jawa
node	5305717364	pertamina	Pertamina	Pertamina	-7.5966689	112.1185215	jawa
node	5305717365	pertamina	Pertamina	Pertamina	-7.6001172	112.1124738	jawa
node	5315819840	pertamina	Pertamina	\\N	-6.520832	106.8402111	jawa
node	5315819841	pertamina	Pertamina	\\N	-6.5284174	106.8495079	jawa
node	5328446018	pertamina	Pertamina	SPBU Pertamina 43.552.18 Jombor	-7.7467999	110.3539732	jawa
node	5351221815	pertamina	Pertamina	Pertamina	-7.5140205	110.5870253	jawa
node	5368373881	other	\\N	\\N	-7.6484518	112.7044083	jawa
node	5382393554	pertamina	Pertamina	SPBU Pertamina 44.574.22 Klepu	-7.6627396	110.6692835	jawa
node	5382406165	pertamina	Pertamina	SPBU Pertamina 44.574.14 Belangwetan	-7.6878787	110.6333685	jawa
node	5388606292	other	\\N	\\N	-6.1945318	106.4248522	sumatra
node	5395341422	other	Blue Gazz	Blue Gazz	-6.1471402	106.7300057	sumatra
node	5408918921	pertamina	Pertamina	Pertamina	-6.9602843	107.7870751	jawa
node	5408953494	pertamina	Pertamina	Pertamina	-8.3153416	114.261471	jawa
node	5411426642	pertamina	Pertamina	SPBU 44.502.01	-7.004337	110.4093897	jawa
node	5421655591	pertamina	PT Pertamina	SPBU 41.502.02	-7.0160106	110.4188204	jawa
node	5423794979	pertamina	PT Pertamina	SPBU 44 50119	-6.9886511	110.3941695	jawa
node	5423969702	pertamina	PT Pertamina	SPBU 41.502.14	-6.9962958	110.4049493	jawa
node	5427962732	pertamina	PT Pertamina	SPBU 44.502.15	-7.0175719	110.3888305	jawa
node	5428067864	pertamina	PT Pertamina	SPBU 44 501 40	-6.9804805	110.3996158	jawa
node	5428721687	pertamina	PT Pertamina	SPBU 44.501.31	-6.9943431	110.4468052	jawa
node	5428721702	pertamina	PT Pertamina	SPBU 44.501.04	-7.0021141	110.4478151	jawa
node	5436543332	pertamina	PT Pertamina	SPBU 44.501.21	-6.9846111	110.4352221	jawa
node	5441523127	pertamina	Pertamina	SPBU Pertamina 54.641.36	-7.8714842	112.0005807	jawa
node	5441924625	pertamina	PT Pertamina	SPBU 45.501.37	-6.9739691	110.434172	jawa
node	5442652671	pertamina	PT Pertamina	SPBU 44.50207	-6.9983244	110.4210198	jawa
node	5444700484	pertamina	PT Pertamina	SPBU 44.501.18	-6.976732	110.4483408	jawa
node	5456071711	pertamina	PT Pertamina	SPBU 41.502.01	-6.9924196	110.4268013	jawa
node	5463951694	other	\\N	\\N	-6.9489868	108.4879424	jawa
node	5463986233	pertamina	PT Pertamina	SPBU 44 501 07	-6.9698511	110.3910837	jawa
node	5466022777	pertamina	PT Pertamina	SPBU 44.501.10	-6.9688806	110.4306176	jawa
node	5466228105	pertamina	PT Pertamina	SPBU 44.501.22	-7.0071969	110.396375	jawa
node	5475150784	pertamina	PT Pertamina	SPBU 44.501.16	-6.9628716	110.4330897	jawa
node	5475580336	pertamina	Pertamina	Pertamina	-6.5139866	106.8633166	jawa
node	5476820923	pertamina	SPBU Bolon	SPBU Bolon	-7.5309553	110.7320402	jawa
node	5478398121	pertamina	Pertamina	SPBU Pertamina 44.577.10 Blulukan	-7.5414039	110.7667295	jawa
node	5492648262	pertamina	PT Pertamina	SPBU 44-501-41	-6.9749908	110.4185736	jawa
node	5504793020	pertamina	PT Pertamina	SPBU 45.502.22	-7.0174974	110.4277655	jawa
node	5509907636	pertamina	PT Pertamina	SPBU 44.501.15	-7.0057666	110.4560924	jawa
node	5509908176	pertamina	PT Pertamina	SPBU 44.501.26	-6.9884808	110.4557625	jawa
node	5510135158	pertamina	PT Pertamina	SPBU 44.501.36	-6.9570355	110.4570794	jawa
node	5510170569	pertamina	PT Pertamina	SPBU 44. 501.09	-6.9855762	110.3812587	jawa
node	5513067916	other	\\N	\\N	-6.8340875	108.5209563	jawa
node	5523913665	pertamina	Pertamina	\\N	-6.3544796	107.3441807	jawa
node	5526778557	pertamina	Pertamina	SPBU Pertamina 44.501.39	-6.9527436	110.4512	jawa
node	5527350497	pertamina	PT Pertamina	SPBU 44.502.12	-7.1077064	110.4123187	jawa
node	5527526834	pertamina	Pertamina	SPBU 44.502.23	-7.0546018	110.4326029	jawa
node	5529534804	pertamina	Pertamina	Pertamina	-7.3339045	112.7293704	jawa
node	5532668061	pertamina	PT Pertamina	SPBU 44.501.11	-6.9539843	110.4733068	jawa
node	5539504858	pertamina	PT Pertamina	SPBU 44.502.16	-7.0573223	110.4607004	jawa
node	5541908442	pertamina	PT Pertamina	SPBU 44.502.03	-7.099576	110.4089034	jawa
node	5544431202	pertamina	SPBU Nagrak	SPBU Nagrak	-7.3244748	108.3295947	jawa
node	5547423782	pertamina	PT Pertamina	SPBU 4450124	-6.9784359	110.4096378	jawa
node	5547760083	pertamina	PT Pertamina	SPBU 44 501 20	-6.9977162	110.379475	jawa
node	5555696901	other	\\N	\\N	-7.5540982	112.2411755	jawa
node	5558508717	pertamina	PT Pertamina	SPBU 44.502.24	-7.0931645	110.326142	jawa
node	5559805868	pertamina	Pertamina	Pertamina	-7.130325	110.4242083	jawa
node	5561483983	pertamina	PT Pertamina	44.501.25	-6.9615324	110.4784888	jawa
node	5566012724	pertamina	PT Pertamina	SPBU 44 501 30	-7.000176	110.3967123	jawa
node	5566054991	pertamina	PT Pertamina	SPBU 44.502.19	-7.080733	110.4115811	jawa
node	5566151736	pertamina	Pertamina	Pertamina	-6.9480256	110.4910469	jawa
node	5566151741	pertamina	Pertamina	Pertamina	-6.9506109	110.4838157	jawa
node	5566321028	pertamina	PT Pertamina	SPBU 44.50217	-7.0627339	110.3109312	jawa
node	5572546842	pertamina	Pertamina	Pertamina	-7.1308684	110.4090115	jawa
node	5580208194	pertamina	Pertamina	SPBU Pertamina 44.501.05 Mangkang	-6.9703902	110.291583	jawa
node	5582145531	pertamina	PT Pertamina	SPBU 44 501 13	-6.9904562	110.3555685	jawa
node	5582160585	pertamina	Pertamina	SPBU 44.502.21	-7.0741904	110.3898418	jawa
node	5582194757	pertamina	Pertamina	SPBU 44.502.13	-7.1111243	110.397605	jawa
node	5583883922	pertamina	SPBU Brawijaya	SPBU Brawijaya	-8.2359771	114.3579813	jawa
node	5585525721	other	\\N	\\N	-8.5616917	114.0954806	jawa
node	5587479433	other	\\N	\\N	-7.6787076	109.6668396	jawa
node	5591359129	pertamina	PT Pertamina	SPBU 44 501 23	-6.9882025	110.3662437	jawa
node	5595941265	pertamina	PT Pertamina	SPBU 44.501.32	-6.9888095	110.4740927	jawa
node	5595991802	pertamina	PT Pertamina	SPBU 44. 501. 29	-6.9729753	110.3140426	jawa
node	5596057981	pertamina	PT Pertamina	44 50114	-6.9848494	110.3379089	jawa
node	5610725589	pertamina	PT Pertamina	SPBU 41.501.28	-7.0164845	110.4868224	jawa
node	5610766731	pertamina	PT Pertamina	SPBU 44 501 27	-6.9791414	110.3336777	jawa
node	5611243499	pertamina	Pertamina	Pertamina	-7.0251656	110.4676447	jawa
node	5611351487	pertamina	Pertamina	SPBU 44 502 25	-7.0845925	110.3610899	jawa
node	5623263231	shell	Shell	Shell	-7.2915588	112.6757492	jawa
node	5623478153	pertamina	Pertamina	SPBU Pertamina 44.501.01 Penggaron	-7.0194953	110.4931578	jawa
node	5623478155	pertamina	Pertamina	Pertamina	-7.0231024	110.4876217	jawa
node	5623648217	pertamina	PT Pertamina	SPBU 44.502.04	-7.042884	110.42232	jawa
node	5636609589	pertamina	PT Pertamina	SPBU 44.502.02	-7.0647356	110.4126406	jawa
node	5639066539	pertamina	PT Pertamina	SPBU 44.501.34	-7.0144346	110.4801464	jawa
node	5639235177	pertamina	PT Pertamina	SPBU 44.502.08	-7.0241446	110.4598448	jawa
node	5641358606	pertamina	PT Pertamina	SPBU 44.502.20	-7.0578799	110.4143491	jawa
node	5650742207	pertamina	Pertamina	Pertamina Gas Station 44.552.13 Caturtunggal	-7.7739826	110.4103696	jawa
node	5650742364	pertamina	Pertamina	SPBU Pertamina 44.552.17 Tajem	-7.7662575	110.4316601	jawa
node	5651175566	pertamina	Pertamina	SPBU Pertamina 44.555.03 Gejayan	-7.7598212	110.3946806	jawa
node	5653012278	pertamina	Pertamina	SPBU 44.502.11	-7.051916	110.4277897	jawa
node	5653520449	pertamina	Pertamina	SPBU Pertamina 44.552.11 Bener	-7.7813087	110.3519602	jawa
node	5668680286	pertamina	Pertamina	Pertamina	-7.0284053	110.3349021	jawa
node	5671317969	pertamina	Pertamina	SPBU Pertamina 44.551.13 Lowanu	-7.8241203	110.3779274	jawa
node	5671688464	pertamina	PT Pertamina	SPBU 44 50 133	-7.0118364	110.3785872	jawa
node	5674565284	pertamina	PT Pertamina	44-50218	-6.9742187	110.3010902	jawa
node	5674565285	pertamina	Pertamina	SPBG Pertamina 41.B.186.01 Mangkang	-6.971519	110.2920189	jawa
node	5702900021	pertamina	SPBU Pertamina 54.628.02 Pejaten	SPBU Pertamina 54.628.02 Pejaten	-7.9140386	113.8400165	jawa
node	5712803180	other	Pertamima	Pertamima	-7.2644498	107.8308894	jawa
node	5713317549	other	\\N	\\N	-7.1474342	107.949736	jawa
node	5713318442	other	\\N	\\N	-7.1632516	107.9512772	jawa
node	5713319731	other	\\N	\\N	-7.1439429	107.9512101	jawa
node	5713319734	other	\\N	\\N	-7.132393	107.9469482	jawa
node	5713319736	other	\\N	\\N	-7.1299571	107.9437207	jawa
node	5713320229	other	\\N	\\N	-7.1612467	107.9512862	jawa
node	5713320232	other	\\N	\\N	-7.1567181	107.9510516	jawa
node	5713321906	other	\\N	\\N	-7.112169	107.9495285	jawa
node	5751447502	pertamina	Pertamina	Pertamina	-7.8446944	112.062505	jawa
node	5761940642	pertamina	Pertamina	SPBU Pertamina 44.507.05	-7.3057599	110.4879029	jawa
node	5772428110	pertamina	Pertamina	SPBU 54.671.28	-7.715748	113.0444509	jawa
node	5772428118	pertamina	Pertamina	SPBU 54.671.05	-7.7157215	113.0563693	jawa
node	5772459496	pertamina	Pertamina	SPBU 54.671.30	-7.7178996	113.0704522	jawa
node	5794030254	pertamina	Pertamina	Pertamina	-6.9041715	107.6650947	jawa
node	5805019402	pertamina	SPBU Pertamina	SPBU Pertamina	-7.7160842	109.7568941	jawa
node	5809461691	pertamina	Pertamina	Pertamina	-6.9910069	107.6319814	jawa
node	5809461696	pertamina	Pertamina	Pertamina	-6.9949827	107.6305194	jawa
node	5810377857	pertamina	Pertamina	Pertamina	-6.9406539	107.7409499	jawa
node	5810382654	pertamina	Pertamina	Pertamina	-7.0414326	107.9682114	jawa
node	5810388958	pertamina	Pertamina	Pertamina	-6.948667	107.7611701	jawa
node	5810389856	pertamina	Pertamina	Pertamina	-7.0360157	107.9265685	jawa
node	5810396553	pertamina	Pertamina	Pertamina	-6.9542368	107.7687646	jawa
node	5812620153	pertamina	SPBU Tenjo	SPBU Tenjo	-6.3289581	106.4494851	jawa
node	5812634578	pertamina	SPBU Tenjo	SPBU Tenjo	-6.3289415	106.4494583	jawa
node	5820668941	pertamina	Pertamina	Pertamina	-6.8405816	108.6372887	jawa
node	5862986949	pertamina	Pertamina	Pertamina	-6.3898086	107.3963758	jawa
node	5872990202	pertamina	Pertamina	SPBU 44.572.22	-7.3974093	111.0650833	jawa
node	5874613351	pertamina	Pertamina	SPBU Pertamina 54.644.01 Barong	-7.603386	112.0207278	jawa
node	5874617692	pertamina	Pertamina	SPBU Pertamina 54.644.14 Kedungsoko	-7.6040884	111.9761207	jawa
node	5875598678	pertamina	Pertamina	Pertamina	-7.538768	110.6176441	jawa
node	5875618523	pertamina	Pertamina	Pertamina	-7.4909836	110.5716841	jawa
node	5875639577	pertamina	Pertamina	Pertamina	-7.46466	110.5549539	jawa
node	5875647357	pertamina	Pertamina	Pertamina	-7.4334101	110.5313585	jawa
node	5875650710	pertamina	Pertamina	Pertamina	-7.4212403	110.5258145	jawa
node	5880960291	other	\\N	\\N	-7.4844915	109.9874136	jawa
node	5886938286	other	\\N	\\N	-8.2165521	114.3243574	jawa
node	5906806702	other	\\N	\\N	-7.2111765	109.8353282	jawa
node	5906806703	other	\\N	\\N	-7.2064957	109.8309798	jawa
node	5906885371	other	\\N	\\N	-7.2055381	109.8247685	jawa
node	5906885374	other	\\N	\\N	-7.2014099	109.8178563	jawa
node	5946695187	pertamina	SPBU KULALET	SPBU KULALET	-7.0061426	107.6148338	jawa
node	5946697287	pertamina	SPBU Pameungpeuk	SPBU Pameungpeuk	-7.0187791	107.6009243	jawa
node	5968057481	pertamina	SPBU	SPBU	-7.3618292	109.9120765	jawa
node	5993873481	pertamina	SPBU Kawedusan	SPBU Kawedusan	-7.6648774	109.6715981	jawa
node	5993873482	pertamina	SPBU Wonoyoso	SPBU Wonoyoso	-7.6643657	109.6605447	jawa
node	5999457185	pertamina	Pertamina	Pertamina	-6.1600505	106.974485	sumatra
node	6047850750	pertamina	Pertamina	\\N	-7.6991084	112.8632179	jawa
node	6087155549	pertamina	SPBU Kubangkangkung	SPBU Kubangkangkung	-7.6176756	108.9352281	jawa
node	6089952646	pertamina	spbu	spbu	-7.8167755	110.0170975	jawa
node	6132439094	pertamina	Pertamina	Pertamina	-7.5686903	112.2366638	jawa
node	6132439095	pertamina	Pertamina	Pertamina	-7.5859743	112.2360244	jawa
node	6132439096	pertamina	Pertamina	Pertamina	-7.6432884	112.2369873	jawa
node	6132439097	other	\\N	\\N	-7.5617147	112.2048866	jawa
node	6136550535	pertamina	Pom Bensin Mojongapit	SPBU Mojongapit (Roda 4)	-7.5377535	112.2501964	jawa
node	6136550536	pertamina	Pom Bensin Mojongapit	SPBU Mojongapit	-7.5376191	112.2501747	jawa
node	6136550537	pertamina	SPBU Pertamina	SPBU Pertamina	-7.530272	112.2361825	jawa
node	6154782886	shell	Shell	Shell	-6.1724952	106.6248661	sumatra
node	6154782887	pertamina	Pertamina	Pertamina	-6.1736868	106.6252595	sumatra
node	6157575311	pertamina	Pertamina	Pertamina	-6.2430891	105.8277052	jawa
node	6165347011	pertamina	Pertamina	Pertamina	-7.2715285	110.3959382	jawa
node	6195003195	pertamina	Pertamina	Pertamina	-7.3678876	110.5633745	jawa
node	6195003196	pertamina	Pertamina	Pertamina	-7.3621831	110.5289591	jawa
node	6203331100	other	pgn	pgn	-6.1661817	106.7931082	sumatra
node	6216122889	pertamina	Pertamina	Pertamina	-6.4044084	106.8187359	jawa
node	6264009591	pertamina	Pertamina	Pertamina 34-15122	-6.1904836	106.6243012	sumatra
node	6337592986	other	Late night gas for Ijen	Late night gas for Ijen	-8.2166348	114.3160783	jawa
node	6344242406	pertamina	Pertamina	Pertamina	-7.8283738	111.9871161	jawa
node	6346813633	other	\\N	\\N	-6.7575766	111.6957369	jawa
node	6400822588	other	\\N	\\N	-6.118815	106.8034509	sumatra
node	6400825386	other	\\N	\\N	-6.1173903	106.8032263	sumatra
node	6426965185	other	\\N	\\N	-6.8186891	108.7093768	jawa
node	6442989486	pertamina	SPBU Tigaraksa	SPBU Tigaraksa	-6.2419691	106.4722441	jawa
node	6443225085	pertamina	SPBU Tigaraksa	SPBU Tigaraksa	-6.2595799	106.4853914	jawa
node	6451087615	other	\\N	\\N	-7.4942083	108.7899102	jawa
node	6474610314	pertamina	Pertamina	SPBU Pertamina 3416935	-6.4892755	106.8444226	jawa
node	6541888483	pertamina	PERTAMINA	SPBU SUKOHARJO	-7.7643441	113.2283673	jawa
node	6601405688	pertamina	SPBU 24-35564	SPBU 24-35564	-5.8481253	105.7313307	sumatra
node	6601797127	pertamina	Pertamina	SPBU 34.403.27	-7.1096121	107.5569758	jawa
node	6611833247	pertamina	SPBU Tersobo	SPBU Tersobo	-7.7185743	109.7807126	jawa
node	6644577557	pertamina	Pertamina	SPBU 54.684.09	-8.1148591	114.4013214	jawa
node	6648798783	pertamina	Pertamina	SPBU Pertamina	-6.203834	106.7172371	jawa
node	6656313290	pertamina	SPBU Banjaran	SPBU Banjaran	-7.0569869	107.5692844	jawa
node	6660138547	pertamina	Pertamina	Pertamina	-6.8561134	107.4909934	jawa
node	6698590914	pertamina	SPBU Tangerang	SPBU Tangerang	-6.2304336	106.5010252	jawa
node	6708887001	other	BP	BP	-6.4652464	107.4440571	jawa
node	6722627348	pertamina	Pertamina	Pertamina	-6.5297462	107.4468758	jawa
node	6722627349	pertamina	Pertamina	Pertamina	-6.5342927	107.4448974	jawa
node	6722627350	pertamina	Pertamina	Pertamina	-6.5283814	107.4354744	jawa
node	6722627351	pertamina	Pertamina	Pertamina	-6.5612268	107.436438	jawa
node	6729074248	pertamina	Pertamina	Pertamina	-6.192772	106.636117	sumatra
node	6729074250	pertamina	Pertamina	Pertamina	-6.1928407	106.6362768	sumatra
node	6729074251	pertamina	Pertamina	Pertamina	-6.1718692	106.6724143	sumatra
node	6731965409	pertamina	Pertamina	Pertamina	-6.2302483	106.5010681	jawa
node	6735711185	pertamina	SPBU Kekiling	SPBU Kekiling	-5.7271251	105.647573	sumatra
node	6738461995	pertamina	Pertamina	Pertamina	-7.6896882	110.0315005	jawa
node	6761427786	other	pom	pom	-7.4974549	112.6888419	jawa
node	6765731714	pertamina	Pertamina	Pertamina	-7.1566982	113.5054867	jawa
node	6765731715	pertamina	Pertamina	Pertamina	-7.1465936	113.4780977	jawa
node	6765777431	pertamina	Pertamina	Pertamina	-7.1336642	113.4773771	jawa
node	6788840485	pertamina	Pertamina	Pertamina	-6.3684827	107.3614504	jawa
node	6788925186	pertamina	Pertamina	Pertamina	-6.9413633	107.5820851	jawa
node	6836235583	pertamina	Pertamina	SPBU Pertamina 44.574.05 Karang	-7.6288995	110.6953252	jawa
node	6836766757	pertamina	Pertamina	Pertamina	-7.417612	112.6939633	jawa
node	6854765639	pertamina	Pertamina	Pertamina	-7.3785908	112.5453601	jawa
node	6854765648	pertamina	Pertamina	Pertamina	-7.3768339	112.5516687	jawa
node	6865964881	pertamina	Pertamina	SPBU Pertamina 34.43111 Baros	-6.964059	106.9368625	jawa
node	6883968485	pertamina	SPBU Pertamina	SPBU Pertamina	-6.115164	106.6807577	sumatra
node	6885754785	pertamina	Pertamina	Pertamina	-8.1522816	114.3966756	jawa
node	6887815985	shell	Shell	Shell	-6.11959	106.6750168	sumatra
node	6900290686	other	\\N	\\N	-6.1957759	106.814329	sumatra
node	6908411685	pertamina	Pertamina	Pertamina	-6.1451683	106.6324439	sumatra
node	6923970719	other	Jakpro	SPBG Ancol	-6.1277186	106.8305692	sumatra
node	6928898487	other	\\N	\\N	-6.1049694	106.6627025	sumatra
node	7022936625	bp	BP	BP	-7.2654852	112.7472246	jawa
node	7045154227	pertamina	Pertamina	Pertamina	-6.2129739	106.9457921	jawa
node	7094881140	pertamina	Pertamina	Pertamina	-7.4811406	110.920007	jawa
node	7094898889	pertamina	Pertamina	Pertamina	-7.4797748	110.919562	jawa
node	7094946326	pertamina	Pertamina	Pertamina	-6.9843987	110.1031148	jawa
node	7094955116	pertamina	Pertamina	Pertamina	-6.9206934	109.8619359	jawa
node	7095019422	pertamina	Pertamina	Pertamina	-6.883918	108.706328	jawa
node	7095019423	pertamina	Pertamina	Pertamina	-6.8817069	108.7043264	jawa
node	7095041099	pertamina	Pertamina	Pertamina	-6.9069799	108.9843705	jawa
node	7095073728	pertamina	Pertamina	Pertamina	-6.9686108	107.703198	jawa
node	7103821392	other	\\N	\\N	-7.7683705	113.1728072	jawa
node	7138972963	pertamina	Pertamina	SPBU Bojongbaru	-6.4817244	106.8084705	jawa
node	7140475082	pertamina	Pertamina	Pertamina	-6.4827398	106.8002173	jawa
node	7203973910	other	\\N	\\N	-6.986709	106.5432963	jawa
node	7257240887	other	pom bensin	pom bensin	-7.5463163	112.6202704	jawa
node	7257240986	other	pom bensin	pom bensin	-7.526207	112.5868177	jawa
node	7270885785	pertamina	Pertamina	Pom Bensin 54.651.01 Genengan	-8.0447519	112.6081086	jawa
node	7320645278	pertamina	Pertamina	SPBU 54.662.30 Terminal Gayatri	-8.0730843	111.8958518	jawa
node	7320645279	pertamina	Pertamina	SPBU Pertamina 54.662.07	-8.0688073	111.8958658	jawa
node	7483344785	pertamina	SPBU Pertamina	SPBU Pertamina	-7.3808502	112.7493351	jawa
node	7606825883	pertamina	SPBU Jingglong	SPBU Jingglong	-7.856766	111.4601185	jawa
node	7689640728	pertamina	SPBU	SPBU	-7.4384687	109.5539143	jawa
node	7724076255	pertamina	SPBU Plumpang	SPBU Plumpang	-7.0291771	112.0953733	jawa
node	7817212285	pertamina	Pertamina	Pertamina	-7.5801686	110.769139	jawa
node	7880510789	pertamina	Pertamina 34-43101	Pertamina 34-43101	-6.9171634	106.9099575	jawa
node	7968495361	pertamina	spbu candisari	spbu candisari	-7.6448899	109.5814057	jawa
node	8025196059	other	Mobil	Mobil	-6.7338697	107.3789673	jawa
node	8025260562	other	Mobil	Mobil	-6.7376281	107.3665326	jawa
node	8032238647	pertamina	Pertamina	Pertamina	-6.866383	107.5375554	jawa
node	8032305647	pertamina	Pertamina	Pertamina	-6.8685756	107.5215635	jawa
node	8038922197	pertamina	Pertamina	Pertamina	-6.8633107	107.5103694	jawa
node	8047376666	pertamina	Pertamina	SPBU Pertamina 54.614.27	-7.5805644	112.156122	jawa
node	8240858248	pertamina	Pertamina	Pertamina	-6.7043837	108.4595445	jawa
node	8244041416	pertamina	Pertamina	SPBU Medalem	-7.1663817	112.0362942	jawa
node	8251375817	pertamina	Pertamina	Pertamina	-8.3559028	114.1280696	jawa
node	8251828512	pertamina	Pertamina	Pertamina	-8.3362313	114.1488458	jawa
node	8273571270	pertamina	Pertamina	Pertamina	-8.3027583	114.2973512	jawa
node	8314735967	pertamina	Pertamina	Pertamina	-6.307395	107.945621	jawa
node	8314743793	pertamina	Pertamina	Pertamina	-6.3120958	107.9825147	jawa
node	8378086473	pertamina	Pertamina	SPBU 34.40504	-6.7421186	107.4415582	jawa
node	8378094595	pertamina	Pertamina	Pertamina	-6.8048145	107.4521717	jawa
node	8378519221	other	\\N	\\N	-7.7432102	110.1427996	jawa
node	8403928575	pertamina	Pertamina	Pertamina	-6.4282323	107.4274833	jawa
node	8421849791	pertamina	Pertamina	SPBU	-7.7612442	109.668424	jawa
node	8421907405	pertamina	Pertamina	Pertamina	-6.3294321	107.3118657	jawa
node	8602219884	other	Bensin Kong Janin	Bensin Kong Janin	-6.2932768	106.9986297	jawa
node	8608714377	other	\\N	\\N	-6.2905477	106.9959735	jawa
node	8717932055	pertamina	Pertamina	SPBU 34.164.17	-6.3812702	106.8085519	jawa
node	8776466317	pertamina	Spbu	Spbu	-6.7604563	108.4544542	jawa
node	8776467118	pertamina	Pertamina	Pertamina 34.456.01	-6.7674159	108.4191421	jawa
node	9013223252	pertamina	Pertamina	SPBU Pertamina 44.557.10 Candimas	-7.8129843	110.2673457	jawa
node	9026135057	pertamina	Pertamina	Pertashop	-7.8256144	110.3450961	jawa
node	9084900661	pertamina	Pertamina	SPBU 44.574.24	-7.6668508	110.7544392	jawa
node	9215112381	pertamina	Pertamina	Pertamina	-6.9592262	110.0156055	jawa
node	9231782387	other	Pom Bensin Jalan Lingkar Salatiga	Pom Bensin Jalan Lingkar Salatiga	-7.336826	110.4772413	jawa
node	9231881356	pertamina	SPBU Lingkar Selatan Salatiga	SPBU Lingkar Selatan Salatiga	-7.3269199	110.479138	jawa
node	9331031205	other	brand:wikidata=Q154950	\\N	-6.2068822	107.0165456	jawa
node	9441736418	pertamina	PT Pertamina (Persero)	SPBU Mini	-6.3324991	106.8342655	jawa
node	9449282573	pertamina	SPBU Pertamina 54.631.09	SPBU Pertamina 54.631.09	-7.5494804	111.6669904	jawa
node	9449282586	pertamina	SPBU Pertamina 54.631.01	SPBU Pertamina 54.631.01	-7.5394959	111.6566344	jawa
node	9468757859	pertamina	Pertamina	\\N	-6.3511647	107.1193008	jawa
node	9502032861	pertamina	Pertamina	Pom Bensin 54.651.81	-8.1152239	112.5570059	jawa
node	9676530860	pertamina	Pertamina	SPBU Pertamina 44.555.20 Wedomartani	-7.7406313	110.4340273	jawa
node	9684871007	pertamina	Pertamina	SPBU Pertamina 44.574.09 Pakis	-7.6076921	110.7018162	jawa
node	9688092587	pertamina	Pertamina	Pertamina	-5.8649972	105.7474667	sumatra
node	9704458484	pertamina	Pertamina	Pertashop	-7.7406704	110.4560001	jawa
node	9717052612	pertamina	Pertamina	Pertamina	-7.3399652	108.5830121	jawa
node	9721921444	pertamina	Pertamina	Pom Bensin 54.661.05	-8.1432341	112.3730497	jawa
node	9725791592	pertamina	Pertamina	Pertamina	-8.1558327	112.5009465	jawa
node	9738011911	pertamina	Pertamina	SPBU Pertamina 54.614.09	-7.5728006	112.1924285	jawa
node	9738011912	pertamina	Pertamina	Pertamina Baron	-7.6062604	112.0553961	jawa
node	9738011913	pertamina	Pertamina	Pertamina	-7.5819101	111.8748587	jawa
node	9738048040	pertamina	Pertamina	Pertamina	-7.5924606	111.8915283	jawa
node	9738049015	pertamina	Pertamina	SPBU Pertamina 54.644.11	-7.6021072	111.9384543	jawa
node	9738051959	pertamina	Pertamina	Pertamina	-7.5644121	111.8409075	jawa
node	9738054209	pertamina	Pertamina	Pertamina	-7.5755198	112.1851522	jawa
node	9738054734	pertamina	Pertamina	Pertamina	-7.5607796	111.7785414	jawa
node	9738054735	akr	AKR	AKR	-7.5531558	111.7022921	jawa
node	9738058117	pertamina	Pertamina	Pertamina	-7.6054947	112.0787862	jawa
node	9742546291	pertamina	Pertamina	Pom Bensin 54.651.36	-8.1313432	112.6790589	jawa
node	9742566301	pertamina	Pertamina	Pom Bensin 54.651.62	-8.1040682	112.6533014	jawa
node	9742627505	other	HMC Yogyakarta	HMC Yogyakarta	-7.7355947	110.4373597	jawa
node	9743439556	pertamina	SPBU 54.631.15 Pilangkenceng	SPBU 54.631.15 Pilangkenceng	-7.494285	111.6111787	jawa
node	9750829949	pertamina	SPBU Pertamina 54.631.22	SPBU Pertamina 54.631.22	-7.5494562	111.6376274	jawa
node	9750829950	pertamina	SPBU 54-63113 Balerejo	SPBU 54-63113 Balerejo	-7.553854	111.5950102	jawa
node	9750829951	pertamina	SPBU Pertamina 54.63108	SPBU Pertamina 54.63108	-7.5932354	111.5375267	jawa
node	9750856896	other	\\N	\\N	-7.5787986	111.6202081	jawa
node	9751888098	pertamina	Pertashop	Pertashop	-8.0015912	110.8337999	jawa
node	9751904502	pertamina	Pertamina	SPBU Pertamina 44.558.15 Patuk	-7.8498434	110.4838603	jawa
node	9751904506	other	\\N	\\N	-7.9992142	110.8346259	jawa
node	9751905448	pertamina	Pertamina	SPBU Pertamina 44.558.13 Sapiturrang	-7.9993945	110.6805601	jawa
node	9751905449	pertamina	Pertamina	Pertamina	-7.9034792	110.8528721	jawa
node	9751913658	other	Subur Mulya	Subur Mulya	-7.8320455	110.9159721	jawa
node	9751915129	pertamina	Pertamina	SPBU Pertamina 44.558.01 Siyono	-7.9491183	110.5854619	jawa
node	9751925444	pertamina	Pertamina	SPBU Pertamina 44.558.11 Mijahan	-7.9864189	110.6260097	jawa
node	9751928772	pertamina	Pertamina	Pertamina	-7.9325022	110.8541542	jawa
node	9751928919	pertamina	Pertamina	SPBU Pertamina 44.558.02 bALEHARJO	-7.9807727	110.6164043	jawa
node	9752208291	pertamina	Pertamina	Pertamina	-7.5475742	110.8898561	jawa
node	9752208293	pertamina	Pertamina	Pertamina	-7.4611354	110.9504037	jawa
node	9752248168	pertamina	Pertamina	Pertamina	-7.4429147	110.9793226	jawa
node	9752255690	pertamina	Pertamina	SPBU Pertamina 44.575.04 Telukan	-7.6225423	110.8222339	jawa
node	9752255746	pertamina	Pertamina	SPBU Pertamina 44.751.29 Sekarpace	-7.5592212	110.8487612	jawa
node	9752257155	pertamina	Pertamina	Pertamina	-7.5322832	110.8989586	jawa
node	9752259602	pertamina	Pertamina	SPBU Pertamina 44.575.07 Kepuh	-7.7136083	110.8610057	jawa
node	9752259725	pertamina	Pertamina	SPBU Pertamina 44.575.17 Bulakrejo	-7.6378948	110.827893	jawa
node	9752259727	pertamina	Pertamina	Pertamina	-7.47751	110.9263775	jawa
node	9752262491	pertamina	Pertashop	Pertashop	-7.565858	110.8686531	jawa
node	9752262493	pertamina	Pertamina	Pertamina	-7.3850953	111.0728495	jawa
node	9752265030	pertamina	Pertamina	SPBU Begajah 44.575.13	-7.7043193	110.8548103	jawa
node	9752950030	pertamina	Pertamina	Pertamina	-7.3669986	111.1140494	jawa
node	9753336819	pertamina	Pertamina	Pertamina	-7.539431	112.391891	jawa
node	9753338261	pertamina	Pertamina	Pertamina	-7.5131866	112.4252409	jawa
node	9753339099	pertamina	Pertamina	SPBU 54.613.26	-7.5120659	112.4288447	jawa
node	9753339102	pertamina	Pertamina	Pertamina	-7.3830729	112.614603	jawa
node	9753339103	pertamina	Pertamina	Pertamina	-7.3661216	112.6614679	jawa
node	9753349862	pertamina	Pertamina	Pertamina	-7.3592173	112.6844588	jawa
node	9753351168	pertamina	Pertamina	Pertamina	-7.4873828	112.4489625	jawa
node	9753354811	pertamina	Pertamina	Pertamina	-7.4555249	112.4594045	jawa
node	9753354812	pertamina	Pertamina	Pertamina	-7.398796	112.5762801	jawa
node	9753358356	pertamina	Pertamina	Pertamina	-7.3949638	112.5894283	jawa
node	9753358357	pertamina	Pertamina	Pertamina	-7.3599941	112.67608	jawa
node	9753359494	pertamina	Pertamina	Pertamina	-7.4298768	112.473776	jawa
node	9753359873	pertamina	Pertamina	Pertamina	-7.5277664	112.4112403	jawa
node	9753359874	pertamina	Pertamina	Pertamina	-7.371634	112.6479341	jawa
node	9753359875	pertamina	Pertamina	Pertamina	-7.3546071	112.694737	jawa
node	9753360174	pertamina	Pertamina	Pertamina	-7.4147859	112.511043	jawa
node	9753360520	pertamina	Pertamina	Pertamina	-7.3560149	112.6938307	jawa
node	9753399368	pertamina	Pertamina	Pertamina	-7.5574861	112.6942535	jawa
node	9753413170	pertamina	Pertamina	Pertamina	-7.775199	112.7442169	jawa
node	9753416531	pertamina	Pertamina	Pertamina	-7.6615625	112.6928272	jawa
node	9753426367	pertamina	Pertamina	Pertamina	-7.657427	112.6889567	jawa
node	9753434318	pertamina	Pertamina	Pertamina	-7.6843363	112.7080364	jawa
node	9753452629	pertamina	Pertamina	Pertamina	-7.7442026	112.7293124	jawa
node	9753454136	pertamina	Pertamina	Pertamina	-7.7944278	112.7378781	jawa
node	9753454137	pertamina	Pertamina	Pertamina	-7.8200681	112.7098728	jawa
node	9765028502	pertamina	SPBU 44 573 07	SPBU 44 573 07	-7.4480784	110.6787785	jawa
node	9767510795	pertamina	SPBU Cilongok	SPBU Cilongok	-6.1724383	106.5475667	sumatra
node	9767605190	pertamina	SPBU Pertamina	SPBU Pertamina	-6.1712397	106.6075563	sumatra
node	9767616678	other	\\N	\\N	-6.171257	106.6096652	sumatra
node	9768597409	pertamina	Pertamina	SPBU Pertamina 54.641.53	-7.7932758	112.0006753	jawa
node	9768597410	pertamina	Pertamina	Pertamina	-7.7742503	111.9870109	jawa
node	9770244858	pertamina	SPBU Pertamina	SPBU Pertamina	-6.1807022	106.5294644	sumatra
node	9771114537	pertamina	SPBU Pertamina	SPBU Pertamina	-6.1892953	106.5936805	sumatra
node	9778852291	pertamina	Pertashop	Pertashop	-7.7446515	110.4037216	jawa
node	9780608421	pertamina	SPBU 54.64130	SPBU 54.64130	-7.7582053	111.9758533	jawa
node	9794564845	pertamina	Pertamina	SPBU Pertamina 44.555.33 Kadisoka	-7.7531937	110.4463703	jawa
node	9797912874	pertamina	Pertamina	SPBU Pertamina 44.574.33 Bowan	-7.6496186	110.6844752	jawa
node	9797919704	pertamina	Pertamina	SPBU Pertamina 44.574.01 Pandan SImping	-7.7471815	110.5249791	jawa
node	9797922261	pertamina	Pertamina	SPBU Pertamina 44.574.16 Kemudo	-7.747453	110.5236743	jawa
node	9797923475	pertamina	Pertamina	SPBU Pertamina 44.574.34 Semangkak	-7.6959733	110.6009482	jawa
node	9797929859	pertamina	Pertamina	SPBU Pertamina 44.574.11 Jogonalan	-7.7247239	110.5543387	jawa
node	9797929925	pertamina	Pertamina	SPBU Pertamina 44.574.13 Kaliwingko	-7.6385209	110.6907808	jawa
node	9797934547	pertamina	Pertamina	Pertamina	-7.5482618	110.7327676	jawa
node	9797935222	pertamina	Pertamina	Pertamina	-7.5461896	110.7250327	jawa
node	9798417621	pertamina	SPBU 54.633.11	SPBU 54.633.11	-7.626187	111.3766472	jawa
node	9798424975	pertamina	SPBU 54.633.01	SPBU 54.633.01	-7.5953998	111.4268891	jawa
node	9798467995	pertamina	Pertamina	Pertamina	-7.6483566	111.3276629	jawa
node	9800491696	pertamina	Pertamina	Pertamina	-7.549954	111.4276166	jawa
node	9800498733	pertamina	Pertamina	Pertamina	-7.4961115	111.4291044	jawa
node	9800951625	pertamina	Pertashop	Pertashop	-7.4775578	111.4619775	jawa
node	9800973709	pertamina	Pertamina	Pertamina	-7.4630143	111.4324974	jawa
node	9800999667	pertamina	Pertamina	SPBU Pertamina 54.632.10 Dadapan	-7.4269199	111.4394838	jawa
node	9801063193	pertamina	Pertamina	SPBU Pertamina 54.632.06 Watualang	-7.4029701	111.4125266	jawa
node	9801066565	pertamina	SPBU 54.632.09	SPBU 54.632.09	-7.3961567	111.3390308	jawa
node	9811810353	pertamina	Pertamina	SPBU Pertamina 44.555.01 Kalasan	-7.7612577	110.4769162	jawa
node	9811839852	pertamina	Pertashop 4P.55549	Pertashop 4P.55549	-7.8141073	110.4427622	jawa
node	9811898451	pertamina	Pertamina	SPBU Pertamina 43.557.15 Palbapang	-7.9100474	110.3183181	jawa
node	9816121467	pertamina	Pertamina	Pertamina	-7.3494233	112.7130857	jawa
node	9821499218	other	Pom bensin Ciledug	Pom bensin Ciledug	-6.2256243	106.7122657	jawa
node	9824969078	pertamina	Pertamina	SPBU Pertamina 44.574.21 Klaten	-7.7118841	110.5933007	jawa
node	9826761617	pertamina	Pertamina	Pertamina	-6.1615784	106.6443195	sumatra
node	9830295683	pertamina	SPBU 43.571.02	SPBU 43.571.02	-7.5613007	110.8176898	jawa
node	9830496617	pertamina	Pertamina	Pertamina	-6.4189118	106.9347397	jawa
node	9830614117	pertamina	Pertamina	Pertamina	-6.4631329	106.8818663	jawa
node	9831896846	pertamina	Pertamina	Terminal BBM Banyuwangi	-8.134104	114.399558	jawa
node	9843385135	pertamina	Pertamina	SPBU Pertamina 44.555.34 Berbah	-7.8050291	110.4460383	jawa
node	9856185298	pertamina	SPBU UNS	SPBU UNS	-7.561174	110.8557665	jawa
node	9857780129	pertamina	Pertamina	SPBU Pertamina 44.555.36 Bimomartani	-7.6896918	110.4617634	jawa
node	9857832261	pertamina	Pertamina	SPBU Pertamina 44.555.38 Pakem Binangun	-7.6585799	110.421743	jawa
node	9864913061	pertamina	Pertamina	Pertamina	-7.0794357	111.0776145	jawa
node	9891041761	pertamina	Pertamina	Pom Bensin 54.651.80	-8.1180082	112.5942387	jawa
node	9906937203	pertamina	Pertamina	Pertamina	-6.1756678	106.7249134	sumatra
node	9916369421	pertamina	Pertamina	SPBG 31-A-125-03	-6.3007864	106.8210646	jawa
node	9916393817	pertamina	Pertamina	SPBG 31-A-125-03	-6.3009789	106.820889	jawa
node	9931605180	other	Pom Bensin Nambangan	Pom Bensin Nambangan	-7.6386517	111.512495	jawa
node	9938554861	other	\\N	\\N	-7.6253666	111.4980869	jawa
node	10015884787	pertamina	Pertamina	Pertamina	-6.498746	106.7948607	jawa
node	10020007923	pertamina	Pertamina	SPBU Pertamina 44.558.07 Baron	-7.9730318	110.6019965	jawa
node	10029821008	other	Mobil	Mobil Indostation	-7.3686253	108.5696987	jawa
node	10041423480	other	Mobil	Mobil Indostation	-7.3714645	108.5092069	jawa
node	10041423482	other	Mobil	Mobil Indostation	-7.3492433	108.6359748	jawa
node	10041423483	other	Mobil	Mobil Indostation	-7.3689654	108.6378014	jawa
node	10041423484	other	Mobil	Mobil Indostation	-7.3859708	108.6036354	jawa
node	10041423486	pertamina	Pertashop	Pertashop	-7.3700135	108.5849324	jawa
node	10048454822	pertamina	Pertamina	SPBU Pertamina 44.555.17 Mindi	-7.7049456	110.431149	jawa
node	10061722976	pertamina	Pertamina	SPBU Pertamina 54.644.02	-7.6015974	112.0930222	jawa
node	10061987885	pertamina	Pertamina	Pertamina	-7.5474426	111.7565269	jawa
node	10069986077	other	Bensin	Bensin	-6.3043608	107.0042021	jawa
node	10110676543	pertamina	Pertamina	SPBU Pertamina 44.574.23 Ngupit	-7.6624043	110.5909495	jawa
node	10125249969	other	\\N	\\N	-6.7388174	108.4944667	jawa
node	10125571835	other	Pom Bensin 54.672.09	Pom Bensin 54.672.09	-7.7618552	113.4001467	jawa
node	10129780494	pertamina	Pertashop Kadirojo	Pertashop Kadirojo	-7.7732452	110.4447163	jawa
node	10166346703	pertamina	Pertamina	Pertashop	-6.8567704	107.5930499	jawa
node	10178928144	pertamina	Pertamina	SPBU Pertamina Purwodadi	-7.8135284	109.996337	jawa
node	10187541949	pertamina	Pertamina	SPBU Pertamina 44.555.16 Balecatur	-7.8072694	110.2910284	jawa
node	10197849122	pertamina	Pertamina	Pertamina	-7.6542142	112.1577479	jawa
node	10282804747	pertamina	SPBU Pertamina	SPBU Mojosongo	-7.544104	110.8400219	jawa
node	10284042092	pertamina	SPBU Pertamina	SPBU Plesungan	-7.5409347	110.8621659	jawa
node	10284575883	pertamina	Pertashop	Pertashop	-8.36566	114.1433417	jawa
node	10284575886	pertamina	Pertamina	Pertamina	-8.4075631	114.1649461	jawa
node	10284614396	pertamina	Pertamina	Pertamina	-8.3011097	114.0293936	jawa
node	10287261599	pertamina	Pertamina	Pertamina	-8.2616108	114.3323562	jawa
node	10288842031	pertamina	Pertamina	Pertamina	-8.4342772	114.3159625	jawa
node	10289687303	pertamina	Pertamina	Pertamina	-7.5534706	112.3010337	jawa
node	10290157012	pertamina	Pertamina	Pertamina	-7.4075555	111.0532453	jawa
node	10292537234	other	Pom Bensin 54.661.09	Pom Bensin 54.661.09	-8.1275127	112.2108912	jawa
node	10293242222	pertamina	Pertamina	SPBU Pertamina 54.661.34 Tanjungsari	-8.0888128	112.1550751	jawa
node	10300017849	pertamina	Pertamina	Pom Bensin 54.651.55	-8.0838728	112.5892964	jawa
node	10300037809	pertamina	Pertamina	Pom Bensin 54.651.54	-8.0872228	112.5880963	jawa
node	10301815406	pertamina	Pertamina	Pertamina	-8.4718052	114.2295651	jawa
node	10303652624	pertamina	Pertamina	Pertamina	-6.9636875	107.6141275	jawa
node	10561643479	pertamina	SPBU Pertamina 35.168.01	SPBU Pertamina 35.168.01	-6.42372	106.9862824	jawa
node	10572892273	pertamina	Pertamina	\\N	-7.6546685	112.853373	jawa
node	10572892274	pertamina	Pertamina	\\N	-7.6538258	112.8551003	jawa
node	10574024062	bp	BP	BP	-7.748119	113.0711925	jawa
node	10574154793	bp	BP	BP	-7.809129	113.1711496	jawa
node	10575251317	pertamina	SPBU Cipayung Jaya	SPBU Cipayung Jaya	-6.4389832	106.7975573	jawa
node	10586058999	pertamina	Pertamina	Pertamina	-6.5758967	106.8384918	jawa
node	10587212637	pertamina	Pertamina	Pertamina	-6.2982712	106.6183539	jawa
node	10595432564	pertamina	PERTASHOP BUMDES NGARGOSARI	PERTASHOP BUMDES NGARGOSARI	-7.6605593	110.1563293	jawa
node	10605641069	pertamina	Pertamina	Pertamina	-8.0619219	112.6008492	jawa
node	10654054456	pertamina	Pertamina	Pertamina	-7.7836176	112.2158924	jawa
node	10711329844	pertamina	Pertamina	SPBU Pertamina 34-16416	-6.403842	106.8376493	jawa
node	10726381605	pertamina	PERTAMINA	PERTAMINA	-7.3931658	110.4235894	jawa
node	10779010555	pertamina	Pertamina	\\N	-8.348416	114.5785813	jawa
node	10779013748	pertamina	Pertamina	SPBU Pertamina 54.822.16	-8.3575859	114.6245958	jawa
node	10779027615	pertamina	Pertamina	Pertamina	-8.366457	114.661583	jawa
node	10779073221	pertamina	Pertamina	\\N	-8.4013944	114.7790503	jawa
node	10817144679	pertamina	Pertamina	SPBU Pertamina 44.592.23	-6.7127599	111.3863757	jawa
node	10869727561	other	Mobil	Mobil Indostation Mustikasari	-6.2899336	107.012814	jawa
node	10869895978	other	\\N	\\N	-6.2905587	107.0140791	jawa
node	10911332387	pertamina	Pertamina	Pertamina	-7.9529044	112.6900171	jawa
node	10988864605	other	\\N	\\N	-6.8825569	106.9459659	jawa
node	10996425232	pertamina	Pertamina	Pertamina SPBU 34-16415	-6.3925169	106.8426475	jawa
node	11002793645	pertamina	Pertamina	Pertamina	-7.0191406	107.6543217	jawa
node	11003271546	pertamina	Pertamina	SPBU Pertamina 54.651.11 Kepanjen	-8.1149751	112.575464	jawa
node	11011422584	pertamina	Pertamina	Pertamina	-8.0686952	112.6398788	jawa
node	11011422588	pertamina	Pertamina	Pertamina	-8.0831785	112.6381531	jawa
node	11021367315	other	\\N	\\N	-6.295214	106.9988388	jawa
node	11050346382	pertamina	Pertamina	Pertamina	-6.9121984	107.6370205	jawa
node	11054959902	pertamina	Pertamina	Pertamina	-6.3306584	106.0805941	jawa
node	11060257721	pertamina	Pertamina	Pertamina 44.561.09 Purwosari	-7.4684635	110.2517152	jawa
node	11101773906	pertamina	Pertamina	SPBU Pertamina 44.575.03 Jombor	-7.6619524	110.8355812	jawa
node	11111659212	pertamina	Pertamina	Pertamina 44 541 14	-7.6731062	110.0379182	jawa
node	11118205830	other	\\N	\\N	-6.2951982	106.9990411	jawa
node	11119893255	pertamina	Pertamina	Pertamina	-6.3389221	105.8303244	jawa
node	11125107237	pertamina	Pertamina	Pertamina	-7.8986402	112.6013617	jawa
node	11152017895	pertamina	SPBU Banyuanyar	SPBU Banyuanyar	-7.5404424	110.8064862	jawa
node	11170227568	pertamina	SPBU Sapen	SPBU Sapen	-7.5827099	110.9072662	jawa
node	11175159042	pertamina	Pertamina	Pertamina	-6.2707542	107.0384914	jawa
node	11178157826	pertamina	SPBU Dagen	SPBU Dagen	-7.5726784	110.8826299	jawa
node	11187212623	other	\\N	\\N	-6.3001384	106.9989763	jawa
node	11198899023	pertamina	Pertamina	Pertamina	-7.6382761	112.0164771	jawa
node	11205301497	other	\\N	\\N	-7.6539167	111.9721321	jawa
node	11205604527	bp	BP	BP	-6.2411643	106.6660036	jawa
node	11215392949	pertamina	Pertamina	SPBU Pertamina Gading 54.644.09	-7.667991	112.0141472	jawa
node	11228248115	pertamina	SPBU Pertamina 54.662.02	SPBU Pertamina 54.662.02	-8.083029	111.949633	jawa
node	11252267237	pertamina	PERTAMINA	PERTAMINA	-7.1559143	112.7176621	jawa
node	11291283261	pertamina	Pertamina	Pertamina	-6.1248443	106.8299134	sumatra
node	11291718924	pertamina	Pertamina	SPBE Pertamina	-6.6383403	106.844691	jawa
node	11323126438	pertamina	pertamina	SPBU Pertamina 54.682.03 Kotakulon	-7.9062646	113.8085652	jawa
node	11323143304	pertamina	SPBU Pertamina Maesan	SPBU Pertamina Maesan	-8.0344948	113.7761636	jawa
node	11323143305	pertamina	Pertamina	SPBU Pertamina Nangkaan	-7.9387317	113.812483	jawa
node	11323198750	pertamina	pertamina	SPBU Pertamina Arjasa	-8.1162411	113.7488324	jawa
node	11323208297	pertamina	Pertamina	Pertamina	-8.1349748	113.7377042	jawa
node	11327552810	pertamina	pertamina	SPBU Pertamina Gajah Mada	-8.178723	113.6794211	jawa
node	11330507793	pertamina	Pertamina	Pertamina Rowotamtu	-8.233911	113.5769163	jawa
node	11330507794	pertamina	SPBU Balung	SPBU Balung	-8.2630516	113.5450832	jawa
node	11337326908	pertamina	Pertamina	Pertamina	-6.9141507	107.5708193	jawa
node	11394387763	other	Pom bensin	Pom bensin	-6.9498208	108.5429664	jawa
node	11416839549	other	Pom Bensin 54.684.19 Karangsari	Pom Bensin 54.684.19 Karangsari	-8.3252963	114.1907194	jawa
node	11427779969	pertamina	SPBU Pertamina 54.673.14	SPBU Pertamina 54.673.14	-8.0855284	113.3319481	jawa
node	11518212031	other	Mobil Indostasion	Mobil Indostasion	-7.6607879	111.0401613	jawa
node	11518326393	pertamina	Pertashop	Pertashop	-7.6525122	111.0457079	jawa
node	11521332625	pertamina	SPBU Bagbagan Pertamina 34 43308	SPBU Bagbagan Pertamina 34 43308	-7.0117072	106.5694696	jawa
node	11554846161	pertamina	Pertamina	SPBU Pertamina 44.555.18 Pedak	-7.7108362	110.4092892	jawa
node	11554890112	pertamina	Pertamina	SPBU Pertamina 44.557.14 UAD	-7.8171055	110.2482589	jawa
node	11595410267	other	\\N	\\N	-7.6378181	111.124325	jawa
node	11595410268	other	\\N	\\N	-7.634752	111.1242424	jawa
node	11683085689	other	\\N	\\N	-7.7024471	111.5729769	jawa
node	11684206973	pertamina	Pertashop	Pertashop	-7.7112896	111.5094931	jawa
node	11738959228	pertamina	SPBU Pertamina 54.651.75 Karanglo	SPBU Pertamina 54.651.75 Karanglo	-7.9206703	112.6524459	jawa
node	11751799554	pertamina	Pertamina	Pertamina	-6.2580165	106.5764098	jawa
node	11754907869	pertamina	SPBU Pertamina	SPBU Pertamina	-6.8907442	110.9882706	jawa
node	11754907870	pertamina	SPBU Pertamina	SPBU Pertamina	-6.8828689	110.9924911	jawa
node	11754920369	pertamina	Pertamina	Pertamina	-6.781011	110.9899383	jawa
node	11754940970	pertamina	Pertamina	Pertamina	-6.8052768	110.9097902	jawa
node	11754946469	pertamina	Pertamina	Pertamina	-6.7895065	110.9775097	jawa
node	11754946470	pertamina	Pertamina	Pertamina	-6.80259	110.9300282	jawa
node	11757199170	pertamina	SPBU Pertamina	SPBU Pertamina	-6.9066674	110.6280613	jawa
node	11757202569	pertamina	Pertamina	Pertamina	-6.9136856	110.6108787	jawa
node	11757217571	pertamina	Pertamina	Pertamina	-6.9225693	110.5824968	jawa
node	11757217576	pertamina	Pertamina	Pertamina	-6.9330711	110.5363825	jawa
node	11757230671	pertamina	SPBU Pertamina	SPBU Pertamina	-6.9596701	110.4440725	jawa
node	11757904155	pertamina	Pertashop	Pertashop	-7.9609241	110.6774085	jawa
node	11758033720	pertamina	Pertamina	SPBU Pertamina 44.558.08 Ngipak	-7.9522945	110.6672389	jawa
node	11777626602	pertamina	SPBU 54.661.19 Selorejo	SPBU 54.661.19 Selorejo	-8.1351307	112.4164762	jawa
node	11779978489	shell	Shell	Shell Swadarma	-6.2376359	106.7599093	jawa
node	11782108497	shell	Shell	Shell	-7.369627	112.7283909	jawa
node	11795810348	other	SPBE Satya Mitra Gas	\\N	-7.0649106	110.4704288	jawa
node	11810827425	pertamina	Pertamina	Pertamina	-7.5476042	111.7581057	jawa
node	11811823269	pertamina	Pertamina	Pertamina	-7.5904209	112.1009659	jawa
node	11823873600	pertamina	Pertamina	SPBU Astanajapura	-6.8065533	108.6146995	jawa
node	11834032419	pertamina	Pertamina	SPBU Tegalsari 44.532.21	-7.4833419	108.7717044	jawa
node	11848023024	pertamina	Pertamina	Pertamina	-6.3092404	106.4482364	jawa
node	11865440380	pertamina	Pertamina	SPBU Pertamina	-6.8710106	108.8395882	jawa
node	11869631702	pertamina	Pertamina	Pertamina	-6.9294651	107.579498	jawa
node	11869640786	pertamina	Pertamina	Pertamina	-6.9224243	107.6306979	jawa
node	11869686069	pertamina	Pertamina	Pertamina	-6.8936046	107.5859941	jawa
node	11905866749	pertamina	Pertamina	Pertamina SPBU 44.544.03	-7.6024887	109.5040567	jawa
node	11906093879	other	OIL!	OIL!	-6.986662	108.3155631	jawa
node	11918159975	pertamina	Pertamina	Pertamina	-6.289393	106.497868	jawa
node	11918160178	pertamina	Pertamina	Pertamina	-6.2880647	106.4717909	jawa
node	11919835983	pertamina	Pertamina	SPBU Pertamina 44.551.16 Jambidan	-7.8437413	110.4106106	jawa
node	11934112698	pertamina	Pertamina	Pertamina	-6.5775073	106.7325528	jawa
node	11940369269	pertamina	SPBU Pertamina Lingkar Timur	SPBU Pertamina Lingkar Timur	-7.4802409	112.7293468	jawa
node	11955845874	other	\\N	\\N	-6.2920478	106.9851265	jawa
node	11964369704	shell	Shell	Shell	-6.5622422	106.7938605	jawa
node	11964911643	pertamina	SPBU Pertamina Bandongan	SPBU Pertamina Bandongan	-7.4664001	110.1909669	jawa
node	11966274069	other	\\N	\\N	-7.6807024	108.6455908	jawa
node	11978321883	pertamina	SPBU Pertamina	SPBU Pertamina	-6.6933162	106.9628678	jawa
node	12011400537	pertamina	SPBU Pertamina 54.684.27	SPBU Pertamina 54.684.27	-8.5121851	114.2542914	jawa
node	12022118688	pertamina	Pertamina	Pertamina 44 543 21	-7.7706614	109.6027021	jawa
node	12040842569	pertamina	SPBU BP	SPBU BP	-6.3759233	106.9044536	jawa
node	12040846279	pertamina	Private Operator	SPBU Pertamina	-6.3748875	106.9145936	jawa
node	12042877883	pertamina	Pertamina	Pertamina	-6.2927688	106.7943644	jawa
node	12062318841	pertamina	SPBU BP	SPBU BP	-6.291496	107.0356145	jawa
node	12079291216	pertamina	Pertamina	SPBU Pertamina 31.431.01 Otista/Tipar Gede	-6.9262784	106.9331071	jawa
node	12079654192	pertamina	Pertamina	Pertamina	-6.8561652	107.1105321	jawa
node	12079685460	pertamina	Pertamina	Pertamina	-6.8049489	107.1649821	jawa
node	12079686330	pertamina	Pertamina	Pertamina	-6.803006	107.1723215	jawa
node	12121336836	pertamina	SPBU PERTAMINA	SPBU PERTAMINA	-6.5168756	107.6671642	jawa
node	12121549809	pertamina	SPBU PERTAMINA KALIJATI	SPBU PERTAMINA KALIJATI	-6.5180519	107.6670489	jawa
node	12148943815	other	Pom Bensin Bogorame Demak	Pom Bensin Bogorame Demak	-6.9003007	110.6327448	jawa
node	12149775855	pertamina	Shell	SPBU Shell	-8.0895178	112.1710754	jawa
node	12164370079	pertamina	SPBU 54.661.36 Bangsri Nglegok	SPBU 54.661.36 Bangsri Nglegok	-8.0560232	112.1805197	jawa
node	12187520992	pertamina	Pertamina	SPBU Pertamina 44.558.04 Semin	-7.8563123	110.7370758	jawa
node	12193573317	pertamina	Pertamina	SPBU Pancawati	-6.3751378	107.3865705	jawa
node	12213179082	shell	Shell	Shell Karawang Barat	-6.3334377	107.2746953	jawa
node	12213787097	pertamina	SPBU pertamina 54.68424	SPBU pertamina 54.68424	-8.4071773	114.2572918	jawa
node	12217012303	pertamina	SPBU Cluring	SPBU Cluring	-8.4322458	114.2073902	jawa
node	12231307601	pertamina	Pertashop	Pertashop	-6.1873638	106.9325041	sumatra
node	12249326309	pertamina	Pertamina	SPBU Pertamina Gumitar	-7.4310698	111.0521657	jawa
node	12293396265	other	Fuel Station	Fuel Station	-8.3711837	114.6256044	jawa
node	12293398520	other	Cửa hàng Xăng dầu	Cửa hàng Xăng dầu	-8.1665389	114.5070504	jawa
node	12293406734	other	Cửa hàng Xăng dầu	Cửa hàng Xăng dầu	-8.1442322	114.5954745	jawa
node	12293406735	other	Fuel Station	Fuel Station	-8.3824644	114.5975123	jawa
node	12293410046	pertamina	SPBU	SPBU	-8.3024075	114.5160832	jawa
node	12293422755	other	Fuel Station	Fuel Station	-8.3608242	114.5718077	jawa
node	12313936324	pertamina	Pertamina	SPBU Pertamina 44.551.22 Bakulan	-7.9116994	110.3630833	jawa
node	12317757141	pertamina	Pertamina	SPBU Pertamina 44.551.21 Bibis	-7.8292175	110.319121	jawa
node	12396693908	other	Mobil	Mobil	-7.6699497	112.1219068	jawa
node	12399511272	pertamina	SPBU Pertamina 34.15409	SPBU Pertamina 34.15409	-6.3603881	106.7483023	jawa
node	12401296985	other	Neste	Neste	-6.9587576	110.7168171	jawa
node	12408564625	pertamina	Pertamina	SPBU Pertamina	-6.2946071	107.3386058	jawa
node	12408807738	pertamina	Pertamina	Pertamina	-6.3163518	107.3161385	jawa
node	12412166815	shell	Shell	Shell	-6.815733	107.6134374	jawa
node	12436972236	pertamina	Pertamina	SPBU Pertamina Dungus	-7.6790579	111.5991508	jawa
node	12458529848	pertamina	Pertamina	SPBU Pertamina 44.532.16 Jetis	-7.707966	109.3690062	jawa
node	12468634966	pertamina	SPBU Pertamina 44.512.06 Limpung	SPBU Pertamina 44.512.06 Limpung	-7.0015282	109.9251742	jawa
node	12509206860	pertamina	Pertamina	Pertamina UNNES	-7.0490755	110.3975341	jawa
node	12509376493	pertamina	Pertamina	Pertashop Karangsari	-7.0968845	110.3819855	jawa
node	12509376506	pertamina	Pertamina	Pertashop Sekaran	-7.0463712	110.3904008	jawa
node	12530836409	pertamina	Pertamina	SPBU Pertamina 44.558.18 Ngawen	-7.8413443	110.6978339	jawa
node	12530836410	pertamina	Pertamina	SPBU Pertamina 43.558.10 Dhaksinarga	-7.9678602	110.6168828	jawa
node	12530836411	pertamina	Pertamina	SPBU Pertamina 44.558.19 Piyaman	-7.9435252	110.6044633	jawa
node	12530836412	pertamina	Pertamina	SPBU Pertamina 44.558.17 Mulo	-8.0346011	110.5846128	jawa
node	12530836413	pertamina	Pertamina	SPBU Pertamina 44.558.16 Karangmojo	-7.936067	110.6846033	jawa
node	12530972290	pertamina	Pertamina	SPBU Pertamina 44.557.16 Pleret	-7.8692655	110.4019069	jawa
node	12531196092	pertamina	Pertamina	SPBU Pertamina 44.555.31 Balong	-7.681099	110.3899184	jawa
node	12531196093	pertamina	Pertamina	SPBU Pertamina 44.555.35 UII	-7.6963399	110.4173303	jawa
node	12531232461	pertamina	Pertamina	SPBU Pertamina 44.556.13 Giripeni	-7.8798134	110.1533276	jawa
node	12531232464	pertamina	Pertamina	SPBU Pertamina 44.556.18 Beji	-7.8592896	110.1524375	jawa
node	12531232465	pertamina	Pertamina	SPBU Pertamina 45.556.15 Barongan Karangsewu	-7.950707	110.2141548	jawa
node	12533292551	pertamina	Pertamina	SPBU Pertamina 44.574.25 Karangnongko	-7.6750881	110.535595	jawa
node	12533292552	pertamina	Pertamina	SPBU Pertamina 45.574.25 Wilis Nglinggi	-7.7026881	110.5761746	jawa
node	12533292553	pertamina	Pertamina	SPBU Pertamina 44.574.26 Gedaren	-7.6413808	110.6000667	jawa
node	12533292554	pertamina	Pertamina	SPBU Pertamina 44.574.07 Jatinom	-7.6305297	110.6038237	jawa
node	12533292560	pertamina	Pertamina	SPBU Pertamina 45.574.31 Tarubasan	-7.6585079	110.6285585	jawa
node	12533292566	pertamina	Pertamina	SPBU Pertamina 45.574.37 Sidowayah	-7.5932141	110.6617542	jawa
node	12533292567	pertamina	Pertamina	SPBU Pertamina 43.574.28 Pandanaran	-7.7735023	110.6152129	jawa
node	12533292568	pertamina	Pertamina	SPBU Pertamina 44.574.15 Danguran	-7.7365004	110.5842163	jawa
node	12533292569	pertamina	Pertamina	SPBU Pertamina 44.574.08 Jatimulyo	-7.700546	110.7200918	jawa
node	12533292570	pertamina	Pertamina	SPBU Pertamina 44.574.30 Wonosari	-7.6139029	110.7301823	jawa
node	12589636372	pertamina	Pertamina	SPBU Pertamina 44.571.26 Nusukan	-7.5507824	110.8296708	jawa
node	12589636373	pertamina	Pertamina	SPBU Pertamina 44.571.17 Pasar Kliwon	-7.5789145	110.831088	jawa
node	12589636374	pertamina	Pertamina	Pertamina	-7.5563772	110.8508353	jawa
node	12589636375	pertamina	Pertamina	SPBU Pertamina 44.571.09 Jebres	-7.5648385	110.8578986	jawa
node	12589636376	pertamina	Pertamina	SPBU Pertamina 44.575.02 Palur	-7.5677972	110.8703412	jawa
node	12597671193	pertamina	SPBU PERTAMINA GAJAH MATI	SPBU PERTAMINA GAJAH MATI	-6.7583805	111.0459561	jawa
node	12597713900	pertamina	SPBU PERTAMINA NGANTRU	SPBU PERTAMINA NGANTRU	-6.76885	111.0534319	jawa
node	12639916346	pertamina	Pertamina	Pertamina	-6.9295816	107.5776794	jawa
node	12639965511	pertamina	Pertamina	Pertamina	-7.1223225	107.4277395	jawa
node	12648524734	pertamina	Pertamina	SPBU Jimbe	-8.1348859	112.134068	jawa
node	12679490401	other	44.543.09 Tamanwinangun	44.543.09 Tamanwinangun	-7.6989227	109.669952	jawa
node	12758084200	pertamina	Pertamina	SPBU Pertamina 34.431.21 Malka Batumas Utama	-6.9507876	106.9301116	jawa
node	12803756827	pertamina	Pertamina	Pertamina	-6.3339074	106.8476908	jawa
node	12820361524	pertamina	Pertamina	SPBU.N 38.422.02	-6.668274	105.5767976	jawa
node	12833776583	pertamina	SPBU BP AKR Jalan Panjang	SPBU BP AKR Jalan Panjang	-6.1799119	106.7667355	sumatra
node	12839335731	other	\\N	\\N	-6.2815508	107.0030204	jawa
node	12853028106	pertamina	Pertamina	Pertamina	-7.2575843	110.4615413	jawa
node	12865200149	pertamina	Spbu klampok	Spbu klampok	-7.4619848	109.4354572	jawa
node	12894115372	pertamina	SPBU	\\N	-7.0935665	110.0837079	jawa
node	12896725842	pertamina	SPBU	\\N	-7.2270887	110.373664	jawa
node	12949380265	pertamina	Pertamina	Pertamina	-6.9695898	106.5210659	jawa
node	12950006088	pertamina	SPBU	\\N	-7.0424381	110.5162489	jawa
node	12965442966	pertamina	Pertamina	SPBU Pertamina 44.576.14 Sambiroto Pracimantoro	-8.0772685	110.8453727	jawa
node	12965442974	pertamina	Pertamina	SPBU Pertamina 44.576.09 Sambiroto Lor	-8.0587132	110.8119056	jawa
node	12965442975	pertamina	Pertamina	SPBU Pertamina 44.576.13 Baturetno	-7.991531	110.9346222	jawa
node	12965667957	pertamina	Pertamina	Pertamina	-7.3469655	108.5179001	jawa
node	12982937748	pertamina	Pertamina	SPBU Pertamina	-6.732557	108.540912	jawa
node	12987391334	shell	Shell	Shell	-6.7372362	108.5494364	jawa
node	12987391344	pertamina	Pertamina	SPBU Pertamina	-6.7385356	108.5484047	jawa
node	12993988149	pertamina	Pertamina	SPBU Pertamina	-6.7099715	108.5648949	jawa
node	13008772350	pertamina	Pertamina	SPBU Pertamina	-6.7258657	108.5207739	jawa
node	13078292416	pertamina	Pertamina	SPBU Pertamina	-6.7239994	108.5729552	jawa
node	13091773322	other	Mobil	Mobil	-6.9144461	107.4618526	jawa
node	13092109772	other	\\N	\\N	-7.6582808	111.4235442	jawa
node	13108680610	pertamina	Pertamina	SPBU Pertamina	-6.7281667	108.538683	jawa
node	13118832901	pertamina	SPBU Pertamina Bunder	SPBU Pertamina Bunder	-6.556348	107.429745	jawa
node	13144172398	pertamina	Pertamina	\\N	-6.9496571	106.4417817	jawa
node	13154970353	pertamina	Pertamina	SPBU Pertamina 44.576.20 Selogiri	-7.7584864	110.88463	jawa
node	13201259585	pertamina	Pertamina	SPBU Pertamina	-6.7643939	108.5284947	jawa
node	13234897857	pertamina	Pertamina	Pertamina 44.577.14	-7.6457179	111.0307999	jawa
node	13239967809	pertamina	Pertamina	Pertamina 54.651.50	-8.094078	112.6966313	jawa
node	13239981647	pertamina	Pertamina	Pertamina 54.651.79	-8.1231739	112.7281035	jawa
node	13239986359	other	Mobil	Mobil Indostation	-8.0958566	112.7366159	jawa
node	13240051276	pertamina	Pertamina	Pertamina	-7.9993992	112.7606646	jawa
node	13240078005	pertamina	Pertamina	Pertamina Gas Station 54.651.61	-8.0239118	112.7673604	jawa
node	13240105659	other	Mobil	Mobil Indostation	-8.0962423	112.7117985	jawa
node	13242246018	pertamina	Pertamina	Pertamina	-6.1216467	106.5791416	sumatra
node	13242246035	pertamina	Pertamina	Pertamina	-6.12897	106.5885106	sumatra
node	13257004678	pertamina	Pertamina	Pertamina 54.651.25	-8.184838	112.6991868	jawa
node	13257004955	pertamina	Pertamina	Pertamina 54.651.58	-8.1820985	112.6931225	jawa
node	13299798204	pertamina	Pertamina	Pertamina	-6.0627927	106.6397944	sumatra
node	13307620684	pertamina	SPBU 51.684.39 Banyuwangi	SPBU 51.684.39 Banyuwangi	-8.1369801	114.3993706	jawa
node	13309103609	pertamina	Pertamina	SPBU Pertamina	-6.3164033	107.3216969	jawa
node	13309103614	pertamina	Pertamina	\\N	-6.2997512	107.2999896	jawa
node	13312125613	pertamina	Pertamina	\\N	-6.3706885	107.3287134	jawa
node	13314070287	pertamina	Pertamina	\\N	-6.2999662	107.2835506	jawa
node	13325266521	akr	AKR	SPBKB AKR COCO Surodadi	-6.9778209	109.9926267	jawa
node	13327372638	pertamina	SPBU Pertamina 44.512.11 Banyuputih	SPBU Pertamina 44.512.11 Banyuputih	-6.9720296	109.9716677	jawa
node	13327387445	pertamina	Pertamina	SPBU Pertamina 44.512.02 Plelen	-6.9707363	110.0185968	jawa
node	13335404000	pertamina	Pertamina	SPBU Pertamina 44.512.09 Penundan	-6.9682954	109.9548571	jawa
node	13337340401	pertamina	Pertashop pertamina	Pertashop pertamina	-7.0715213	110.4279544	jawa
node	13337340501	pertamina	Pertashop Grafika	Pertashop Grafika	-7.0789252	110.4211745	jawa
node	13339719694	pertamina	Vivo	SPBU VIVO Batu Tulis	-6.6192518	106.8069317	jawa
node	13359184005	pertamina	Pertamina	Pertamina	-6.2298756	106.7229775	jawa
node	13359184306	shell	Shell	Shell	-6.2267613	106.714415	jawa
node	13361269702	pertamina	SPBU Shell Pondok Cabe	SPBU Shell Pondok Cabe	-6.3469855	106.7651078	jawa
node	13365858988	other	\\N	\\N	-7.8156701	110.3742102	jawa
node	13368863801	pertamina	Pertamina	Pertamina	-6.2778319	106.4817753	jawa
node	13381840529	pertamina	Pertamina	Pertamina	-6.8187418	107.4763625	jawa
node	13394308083	bp	BP	BP	-6.2001176	106.7044416	jawa
node	13410108329	pertamina	SPBU Pertamina 54.662.20 Pinggirsari	SPBU Pertamina 54.662.20 Pinggirsari	-8.0322836	111.9468484	jawa
node	13412509828	other	\\N	\\N	-6.0193704	106.0526444	sumatra
node	13418219331	other	\\N	\\N	-6.0137511	105.9807437	sumatra
node	13418219371	other	\\N	\\N	-6.0154756	105.976404	sumatra
node	13418226935	other	\\N	\\N	-6.010074	106.0215266	sumatra
node	13432952878	shell	Shell	Shell	-6.7558369	108.558751	jawa
node	13457283801	other	BP	BP	-6.1453118	106.7066724	sumatra
node	13457284601	shell	Shell	Shell	-6.1470355	106.7025981	sumatra
node	13457375202	shell	Shell	Shell	-6.1470541	106.7025887	sumatra
node	13457385101	other	BP	BP	-6.1453709	106.7066706	sumatra
node	13503287478	pertamina	Pertamina	SPBU Krapyak	-6.6007792	110.6649164	jawa
node	13503334767	pertamina	Pertamina	SPBU Ujungbatu	-6.5840402	110.6656657	jawa
node	13503334772	pertamina	Pertamina	SPBU Mulyoharjo	-6.5847701	110.6781513	jawa
node	13504049994	pertamina	Pertamina	SPBU Nalumsari	-6.7581265	110.766081	jawa
node	13526291947	other	\\N	\\N	-6.2455302	106.5288844	jawa
node	13529699572	bp	BP	BP	-6.3899547	106.9493581	jawa
node	13529699588	pertamina	Pertamina	Pertamina	-6.3923922	106.9559579	jawa
node	13535584898	pertamina	Pertamina	SPBU Pertamina	-6.7106942	108.5411983	jawa
node	13539946519	pertamina	Pertamina	SPBU Pertamina	-6.7573079	108.5579943	jawa
node	13539946520	pertamina	Pertamina	SPBU Pertamina	-6.7465279	108.586826	jawa
node	13539946521	pertamina	Pertamina	SPBU Pertamina	-6.7422558	108.5693166	jawa
node	13539946522	pertamina	Pertamina	SPBU Pertamina	-6.7349753	108.5621631	jawa
node	13539946523	pertamina	Pertamina	SPBU Pertamina	-6.74879	108.5265655	jawa
node	13539946524	pertamina	Pertamina	SPBU Pertamina	-6.7360311	108.5800648	jawa
node	13539946525	pertamina	Pertamina	SPBU Pertamina	-6.6982018	108.5551659	jawa
node	13539946577	pertamina	Pertamina	SPBU Pertamina	-6.710413	108.5353678	jawa
node	13563275755	pertamina	SPBU Pertamina 54.612.50 Jalan Diponegoro	SPBU Pertamina 54.612.50 Jalan Diponegoro	-7.4519438	112.7153972	jawa
node	13610106551	pertamina	SPBU Tambak Bayan	SPBU Tambak Bayan	-7.8652473	111.450628	jawa
node	13610199565	pertamina	SPBU Tambak Bayan	SPBU Tambak Bayan	-7.8652452	111.4506278	jawa
node	13646945861	pertamina	Pertamina	SPBU Pertamina 44.593.06 Gubug	-7.0511459	110.6533016	jawa
node	13646945888	pertamina	Pertamina	SPBU Pertamina 44.581.02 Godong	-7.0203771	110.6986478	jawa
node	13657288188	pertamina	SPBU Pertamina 54.651.42 Pagelaran	SPBU Pertamina 54.651.42 Pagelaran	-8.1921858	112.6228182	jawa
node	13660587274	pertamina	Pom bensin pertamina	Pom bensin pertamina	-7.8308526	112.0424327	jawa
node	13672133099	pertamina	Pertamina	SPBU 54.632.20 Mojomanis	-7.5339392	111.5362048	jawa
node	13673331572	pertamina	Pertamina	SPBU Pertamina 54.632.16 Ngantru	-7.3853443	111.4694029	jawa
node	13685972278	pertamina	Pertamina	SPBU Jalaksana 34.45517	-6.9258187	108.486977	jawa
node	13686002317	other	\\N	\\N	-6.9480165	108.5401829	jawa
node	13686287219	pertamina	Pertamina	SPBU Pertamina	-6.7733328	108.5203031	jawa
node	13686302316	other	\\N	\\N	-6.9749836	108.4845657	jawa
node	13707415092	pertamina	Pertashop	Pertashop	-6.838466	107.8954941	jawa
node	13707453645	pertamina	Pertamina	Pertamina	-6.8629652	107.897721	jawa
node	13710086856	pertamina	Pertamina	Pertamina	-6.7403946	107.7965937	jawa
node	13719993310	pertamina	Pertamina	Pertamina	-6.3506461	107.234842	jawa
node	13724471011	pertamina	SPBU Pertamina 44.595.27	SPBU Pertamina 44.595.27	-6.8894256	110.6338529	jawa
node	13724472923	pertamina	SPBU Pertamina 44.595.19	SPBU Pertamina 44.595.19	-6.859585	110.7847471	jawa
node	13738962201	pertamina	Pertamina	Pertamina	-7.3083705	110.4654517	jawa
node	13741805801	bp	bp Ultimate	bp Ultimate	-6.1757306	106.8013849	sumatra
node	13744267850	pertamina	Pertamina	Pertamina	-6.8041619	110.8610309	jawa
node	13783914455	other	Petrashop 3P.16102	Petrashop 3P.16102	-6.5341877	106.7862982	jawa
node	13787259970	pertamina	Pertamina	SPBU PERTAMINA 44.502.35 BSB BARAT	-7.0389217	110.3301878	jawa
node	13796130757	pertamina	Pertamina	Pertamina	-6.9362243	107.7006288	jawa
node	13848009207	bp	BP	BP	-7.2602122	112.7364068	jawa
node	13898919958	other	Mobil	Mobil	-6.4496493	106.7205477	jawa
node	13917049868	pertamina	SPBU 34-46101	SPBU 34-46101	-7.2743656	108.1932429	jawa
node	13917417655	pertamina	SPBU 34-46114	SPBU 34-46114	-7.3147095	108.1988466	jawa
node	13917797514	pertamina	SPBU 34-46118	SPBU 34-46118	-7.3286611	108.2066511	jawa
node	13918913136	pertamina	SPBU Segawe	SPBU Segawe	-6.5076806	110.7977639	jawa
node	13919156905	pertamina	SPBU Sekuro	SPBU Sekuro	-6.5291247	110.7298104	jawa
node	13928662646	pertamina	SPBU Mlonggo	SPBU Mlonggo	-6.5282905	110.7030429	jawa
node	13932654501	pertamina	SPBU Cicadas	SPBU Cicadas	-6.5528866	107.7488465	jawa
node	13943027801	pertamina	Pertamina	SPBU Pertamina 44.531.28 Kedungpring Kemranjen	-7.5993742	109.333642	jawa
node	13946559114	pertamina	SPBU 43.543.16 Petanahan	SPBU 43.543.16 Petanahan	-7.7423327	109.5868366	jawa
node	13946747760	pertamina	SPBU Ngebruk Juwana	SPBU Ngebruk Juwana	-6.7159361	111.1548185	jawa
node	13946811291	pertamina	SPBU Jakenan	SPBU Jakenan	-6.7578807	111.1555864	jawa
node	13946854538	pertamina	SPBU Jaken	SPBU Jaken	-6.7753829	111.235609	jawa
node	13946989179	pertamina	SPBU Ngroto	SPBU Ngroto	-6.7015803	110.7633247	jawa
node	13946991205	pertamina	SPBU Mindahan Kidul	SPBU Mindahan Kidul	-6.6625548	110.7543097	jawa
node	13947006817	pertamina	SPBU Lebak	SPBU Lebak	-6.5942521	110.7437343	jawa
node	13955736801	bp	BP AKR	BP AKR	-6.1722805	106.9620682	sumatra
node	13955771545	other	Mobil	Mobil	-6.3133407	106.6606678	jawa
node	13966393201	pertamina	Pertamina	Pertamina	-6.1619971	106.9898752	sumatra
node	13969421804	pertamina	SPBU Krasak	SPBU Krasak	-6.5302171	110.75062	jawa
node	13969521273	pertamina	SPBU Mambak	SPBU Mambak	-6.5624255	110.690324	jawa
node	13969572364	pertamina	SPBU Srobyong	SPBU Srobyong	-6.5148413	110.7064893	jawa
node	13980324892	bp	BP	BP	-6.320936	106.8361941	jawa
node	13982916016	other	\\N	\\N	-6.801984	107.091002	jawa
node	13982916018	other	\\N	\\N	-6.5732342	107.4627525	jawa
node	13982916346	other	\\N	\\N	-6.8106993	107.1253864	jawa
node	13982918882	other	\\N	\\N	-6.6668073	107.547585	jawa
node	13982922157	other	\\N	\\N	-6.6030423	107.41448	jawa
node	13982926753	other	\\N	\\N	-6.7661662	107.2261674	jawa
node	13982935764	other	\\N	\\N	-6.8022452	107.1069372	jawa
node	13982935765	other	\\N	\\N	-6.5998658	107.4840577	jawa
node	13982944467	other	\\N	\\N	-6.7620206	107.0513619	jawa
node	13982945505	other	\\N	\\N	-6.5954462	107.4253512	jawa
node	13985884280	other	\\N	\\N	-6.9525113	107.6379289	jawa
node	13985884435	other	\\N	\\N	-7.0139794	107.8028758	jawa
node	13985884695	other	\\N	\\N	-7.0356101	107.7771293	jawa
node	13985894468	other	\\N	\\N	-7.3711811	108.08737	jawa
node	13985895069	other	\\N	\\N	-7.1167796	107.8959577	jawa
node	13985895224	other	\\N	\\N	-7.3463084	108.1882884	jawa
node	13985896098	other	\\N	\\N	-7.0327207	107.7822808	jawa
node	13985901057	other	\\N	\\N	-7.342701	108.161618	jawa
node	13985908145	other	\\N	\\N	-7.0093557	107.7230577	jawa
node	13985908209	other	\\N	\\N	-6.8295596	107.6399759	jawa
node	13985908247	other	\\N	\\N	-7.1476241	107.899336	jawa
node	13985909773	other	\\N	\\N	-7.2695726	107.914185	jawa
node	13985912592	other	\\N	\\N	-6.9602487	107.6631172	jawa
node	13985912802	other	\\N	\\N	-7.2370584	107.9076086	jawa
node	13986240071	pertamina	SPBU Duren	SPBU Duren	-8.1243192	112.2825749	jawa
node	13986263450	pertamina	Pertamina SPBU 54 634 07, Pom Jetis Ponorogo	Pertamina SPBU 54 634 07, Pom Jetis Ponorogo	-7.9372099	111.4794242	jawa
node	13986281108	pertamina	SPBU 54.662.12 Rejotangan	SPBU 54.662.12 Rejotangan	-8.1182126	112.0871911	jawa
node	13987974548	other	\\N	\\N	-7.3548038	108.2727579	jawa
node	13987978406	other	\\N	\\N	-7.3573471	108.8424387	jawa
node	13987978407	other	\\N	\\N	-7.5385261	109.1453378	jawa
node	13987980153	other	\\N	\\N	-7.5156477	109.0463055	jawa
node	13987980154	other	\\N	\\N	-7.4718599	109.2197785	jawa
node	13987980986	other	\\N	\\N	-7.298542	108.776484	jawa
node	13987981092	other	\\N	\\N	-7.3491361	108.3008711	jawa
node	13987982187	other	\\N	\\N	-7.3777813	108.8695053	jawa
node	13987982561	other	\\N	\\N	-7.3337065	108.6759371	jawa
node	13987983713	other	\\N	\\N	-7.4894153	108.9967535	jawa
node	13987985295	other	\\N	\\N	-7.39859	109.2449721	jawa
node	13987986906	other	\\N	\\N	-7.4152858	108.906266	jawa
node	13987991455	other	\\N	\\N	-7.3730362	108.4620461	jawa
node	13987997334	other	\\N	\\N	-7.4409053	109.2261245	jawa
node	13987997641	other	\\N	\\N	-7.5304599	109.0981824	jawa
node	13987997973	other	\\N	\\N	-7.3014501	108.7541432	jawa
node	13987997974	other	\\N	\\N	-7.3059086	108.7930079	jawa
node	13988008384	other	\\N	\\N	-7.5297897	109.190617	jawa
node	13989992373	other	\\N	\\N	-7.4096436	109.1532698	jawa
node	13990014165	other	\\N	\\N	-7.4206911	109.2044397	jawa
node	13990040865	other	\\N	\\N	-7.2292141	109.2869228	jawa
node	13990076544	other	\\N	\\N	-7.4226311	109.2128933	jawa
node	13990083258	other	\\N	\\N	-7.4223213	109.2119039	jawa
node	13991576536	pertamina	SPBU Bajang	SPBU Bajang	-7.9076424	111.4858992	jawa
node	13992312389	other	\\N	\\N	-7.2853241	109.0422948	jawa
node	13992342356	other	\\N	\\N	-7.3034805	109.0412563	jawa
node	13992357986	other	\\N	\\N	-7.449419	109.5316933	jawa
node	13992358198	other	\\N	\\N	-7.4007122	109.7753866	jawa
node	13992361016	other	\\N	\\N	-7.3965622	109.6978641	jawa
node	13992364008	other	\\N	\\N	-7.3036187	109.0410526	jawa
node	13992364009	other	\\N	\\N	-7.446219	109.4226647	jawa
node	13992364010	other	\\N	\\N	-7.4250083	109.8485131	jawa
node	13992368253	other	\\N	\\N	-7.3998327	109.1087068	jawa
node	13992372951	other	\\N	\\N	-7.405438	109.0756787	jawa
node	13995000595	pertamina	Pertamina	Pertashop	-7.285009	109.9164187	jawa
node	13995000597	other	\\N	\\N	-7.5279533	110.2299773	jawa
node	13995007796	other	\\N	\\N	-7.3344271	110.2045697	jawa
node	13995010749	other	\\N	\\N	-7.6593989	110.6284275	jawa
node	13995012355	pertamina	Pertamina	Pertashop	-7.2664267	109.9577086	jawa
node	13995029222	pertamina	Pertamina	Pertashop	-7.5629482	110.6200219	jawa
node	13995030995	other	\\N	\\N	-7.2926847	110.1739067	jawa
node	13995037536	pertamina	Pertamina	Pertashop	-7.246823	110.0327302	jawa
node	13995039434	pertamina	Pertamina	Pertashop	-7.2132432	109.9274305	jawa
node	13995040542	pertamina	Pertamina	Pertashop	-7.5152801	110.5217047	jawa
node	13995043752	pertamina	Pertamina	Pertashop	-7.2716841	109.9855398	jawa
node	13995043760	other	\\N	\\N	-7.5272568	110.5566002	jawa
node	13995044516	other	\\N	\\N	-7.2578902	110.0725532	jawa
node	13995044517	other	\\N	\\N	-7.4562504	110.2231381	jawa
node	13995044518	other	\\N	\\N	-7.549686	110.2443817	jawa
node	13995050825	other	\\N	\\N	-7.3719009	110.2188163	jawa
node	13995051147	pertamina	Pertamina	Pertashop	-7.4980043	110.4156317	jawa
node	13995051674	pertamina	Pertamina	Pertashop	-7.1994392	109.8810256	jawa
node	13995051675	pertamina	Pertamina	Pertashop	-7.2388539	110.0521921	jawa
node	13995051803	other	\\N	\\N	-7.2767351	110.1497772	jawa
node	13995051971	pertamina	Pertamina	Pertashop	-7.5497997	110.260039	jawa
node	13997678845	pertamina	Pertamina	Pertashop	-7.9840879	110.5379126	jawa
node	13997681334	pertamina	Pertamina	Pertashop	-7.8779784	110.4552106	jawa
node	13997687179	pertamina	Pertamina	Pertashop	-7.8613937	110.4727179	jawa
node	13997688264	pertamina	Pertamina	Pertashop	-7.9518939	110.5376905	jawa
node	13997695017	pertamina	Pertamina	Pertashop	-7.9639651	110.5137548	jawa
node	13997705037	other	\\N	\\N	-8.0418949	110.5183168	jawa
node	13997712902	pertamina	Pertamina	Pertashop	-8.0005943	110.5132843	jawa
node	13997715978	other	\\N	\\N	-7.7143418	110.6010096	jawa
node	13999763457	pertamina	Pertamina	Pertashop	-7.7295305	110.4080998	jawa
node	13999878751	pertamina	Pertamina	Pertashop	-7.6396317	110.3588703	jawa
node	13999879149	pertamina	Pertamina	Pertashop	-7.8423864	110.1006525	jawa
node	13999910900	pertamina	Pertamina	Pertashop	-7.718894	110.1211109	jawa
node	13999914725	pertamina	Pertamina	Pertashop	-7.6605702	110.1565811	jawa
node	13999923708	pertamina	Pertamina	Pertashop	-7.6605702	110.1565811	jawa
node	14002327068	other	\\N	\\N	-7.6097522	110.9855597	jawa
node	14002352321	other	\\N	\\N	-7.6171495	111.0065105	jawa
node	14002352322	other	\\N	\\N	-7.659449	111.1127098	jawa
node	14002354083	pertamina	Pertamina	Pertashop	-7.6680024	111.1180947	jawa
node	14002354388	pertamina	Pertamina	Pertashop	-7.5982261	110.9553557	jawa
node	14002355517	pertamina	Pertamina	Pertashop	-7.6341502	111.0989476	jawa
node	14002356314	other	\\N	\\N	-7.5960206	110.9359533	jawa
node	14002356655	pertamina	Pertamina	Pertashop	-7.7066213	110.6209495	jawa
node	14002358245	other	\\N	\\N	-7.6154271	111.0560959	jawa
node	14004425272	pertamina	Pertamina	Pertashop	-8.2002901	111.7277515	jawa
node	14004450687	other	\\N	\\N	-7.9845903	111.5751987	jawa
node	14004450688	pertamina	Pertamina	Pertashop	-8.1346755	111.7937309	jawa
node	14004472684	other	\\N	\\N	-7.9060982	111.4613679	jawa
node	14004473780	pertamina	Pertamina	Pertashop	-7.6913776	111.2555213	jawa
node	14004485799	pertamina	Pertamina	Pertashop	-8.1851818	111.741269	jawa
node	14004493477	pertamina	Pertamina	Pertashop	-7.7833302	111.4356624	jawa
node	14004503259	other	\\N	\\N	-7.964236	111.5311238	jawa
node	14004504252	other	\\N	\\N	-7.8378856	111.4681426	jawa
node	14004507764	pertamina	Pertamina	Pertashop	-8.2213066	111.7164859	jawa
node	14004508657	other	\\N	\\N	-7.7614733	111.4026159	jawa
node	14004508658	pertamina	Pertamina	Pertashop	-8.0295504	111.6054937	jawa
node	14004510933	pertamina	Pertamina	Pertashop	-8.2622813	111.7017573	jawa
node	14004511253	pertamina	Pertamina	Pertashop	-7.7182712	111.2702545	jawa
node	14004511942	pertamina	Pertamina	Pertashop	-7.7538833	111.3637494	jawa
node	14004512233	other	\\N	\\N	-8.1064284	111.7634551	jawa
node	14004513371	pertamina	Pertamina	Pertashop	-7.6704047	111.2286843	jawa
node	14004513804	pertamina	Pertamina	Pertashop	-7.9617217	111.5135259	jawa
node	14004514428	other	\\N	\\N	-8.0887635	111.7133934	jawa
node	14004515749	other	\\N	\\N	-7.747587	111.3355518	jawa
node	14004515750	other	\\N	\\N	-8.151481	111.7889924	jawa
node	14005947095	other	\\N	\\N	-8.286372	112.2449225	jawa
node	14006237478	other	\\N	\\N	-8.246346	112.2805519	jawa
node	14006269176	pertamina	Pertamina	Pertashop	-8.1868171	112.2558157	jawa
node	14006269750	pertamina	Pertamina	Pertashop	-8.3291223	112.4422142	jawa
node	14006273601	other	\\N	\\N	-8.2622459	112.3507782	jawa
node	14006280977	other	\\N	\\N	-8.2824355	111.7236219	jawa
node	14008384518	pertamina	SPBU Kelet	SPBU Kelet	-6.4931037	110.8980764	jawa
node	14008414102	pertamina	SPBU Mojo	SPBU Mojo	-6.506072	110.9128944	jawa
node	14008443093	bp	BP	BP	-7.8943642	112.5525738	jawa
node	14008453892	other	\\N	\\N	-8.1716886	112.6254568	jawa
node	14008460121	pertamina	Pertamina	Pertashop	-8.0082124	112.5648498	jawa
node	14008461428	other	\\N	\\N	-8.4213851	112.6864486	jawa
node	14008462829	pertamina	Pertamina	Pertashop	-8.2488842	112.6765386	jawa
node	14008468204	pertamina	Pertamina	Pertashop	-8.273828	112.6910933	jawa
node	14008468707	pertamina	Pertamina	Pertashop	-8.0169064	112.5978394	jawa
node	14008469438	pertamina	Pertamina	Pertashop	-8.3755407	112.6864001	jawa
node	14008469439	other	\\N	\\N	-8.2662789	112.6936491	jawa
node	14008469601	pertamina	Pertamina	Pertashop	-8.1565749	112.5948584	jawa
node	14008530088	bp	BP	BP	-6.3862183	106.9493765	jawa
node	14008616195	other	\\N	\\N	-6.8405546	110.8323836	jawa
node	14008639928	pertamina	SPBU Prambatan	SPBU Prambatan	-6.8016535	110.8240605	jawa
node	14014525089	pertamina	Pertamina	Pertashop	-7.7644965	112.5275969	jawa
node	14014535496	pertamina	Pertamina	Pertashop	-7.8486911	112.3732822	jawa
node	14014545941	other	\\N	\\N	-7.8724638	112.3767514	jawa
node	14014555695	other	\\N	\\N	-7.8483568	112.4776671	jawa
node	14014558781	other	\\N	\\N	-7.6118872	112.4819943	jawa
node	14014560452	pertamina	Pertamina	Pertashop	-7.8337967	112.5285856	jawa
node	14014561078	other	\\N	\\N	-7.7462849	112.280005	jawa
node	14014571201	pertamina	Pertamina	Pertashop	-7.7836029	112.3331765	jawa
node	14016694035	other	\\N	\\N	-7.1154569	111.8078345	jawa
node	14017333829	other	\\N	\\N	-7.9735245	112.7403343	jawa
node	14017336552	pertamina	Pertamina	Pertashop	-8.0886617	113.0868904	jawa
node	14017342045	other	\\N	\\N	-7.1124414	111.8071043	jawa
node	14017348632	pertamina	Pertamina	Pertashop	-8.0266239	112.8267662	jawa
node	14017380421	other	\\N	\\N	-7.9735746	112.7396874	jawa
node	14019699944	pertamina	Pertamina	Pertashop	-7.9204572	112.9980621	jawa
node	14021787938	pertamina	Pertamina	Pertashop	-7.8879159	113.3416368	jawa
node	14021787939	pertamina	Pertamina	Pertashop	-7.8235895	113.7598517	jawa
node	14021827852	pertamina	Pertamina	Pertashop	-7.7434612	113.5302573	jawa
node	14021850388	pertamina	Pertamina	Pertashop	-7.8411342	113.1272849	jawa
node	14021850762	pertamina	Pertamina	Pertashop	-7.8457699	113.4367937	jawa
node	14021853569	pertamina	Pertamina	Pertashop	-7.9509578	113.4008499	jawa
node	14021858026	pertamina	Pertamina	Pertashop	-7.8436564	113.473351	jawa
node	14021864444	other	\\N	\\N	-7.7180945	113.527944	jawa
node	14021865859	other	\\N	\\N	-7.7356032	113.6785927	jawa
node	14021879507	pertamina	Pertamina	Pertashop	-7.7330586	113.7100456	jawa
node	14021886897	pertamina	Pertamina	Pertashop	-7.9417706	113.4398358	jawa
node	14021889422	pertamina	Pertamina	Pertashop	-7.8587151	113.3052273	jawa
node	14027696572	pertamina	SPBU Welahan	SPBU Welahan	-6.7683808	110.7249327	jawa
node	14027778981	pertamina	SPBU Modern Bakung	SPBU Modern Bakung	-6.8244921	110.7268668	jawa
node	14027813343	pertamina	SPBU Pertamina Kriyan	SPBU Pertamina Kriyan	-6.7341867	110.7243846	jawa
node	14027813344	pertamina	Pertamina	SPBU Sengon	-6.7337331	110.7327278	jawa
node	14027813345	pertamina	Pertamina	SPBU Krasak	-6.7169796	110.7164987	jawa
node	14027834512	pertamina	Pertamina	SPBU Troso	-6.6704778	110.7116906	jawa
node	14027834519	pertamina	Pertamina	SPBU Ngabul	-6.6481359	110.7104776	jawa
node	14027907556	pertamina	SPBU Kalitekuk	SPBU Kalitekuk	-6.6325002	110.7045756	jawa
node	14028606416	pertamina	Pertamina	Pertashop	-7.9526289	113.9941185	jawa
node	14028606623	pertamina	Pertamina	Pertashop	-7.9420282	113.9633888	jawa
node	14030006835	pertamina	Pertamina	Pertashop	-7.9549293	113.9782894	jawa
node	14030344139	other	\\N	\\N	-7.1152167	111.8039332	jawa
way	119430783	pertamina	SPBU Kelapa 2	SPBU Kelapa 2	-6.3551304	106.845806	jawa
way	119724950	pertamina	SPBU Kelapa 2 Pal	SPBU Kelapa 2 Pal	-6.3553255	106.8540078	jawa
way	121356429	pertamina	Pertamina	SPBU 34-13416	-6.2276739	106.9263208	jawa
way	122830465	pertamina	Pertamina	SPBU Pertamina 44.552.06 Terban	-7.7821568	110.3721445	jawa
way	123317405	shell	Shell	Shell Raden Inten	-6.2340783	106.9232038	jawa
way	142570036	pertamina	Pertamina	SPBU 34-16117	-6.5978104	106.7789765	jawa
way	151411140	pertamina	Pertamina	\\N	-6.404397	106.8186923	jawa
way	151418269	pertamina	Pertamina	SPBU 34-16416	-6.4040925	106.8375772	jawa
way	155937902	pertamina	Pertamina	SPBU 34.133.07	-6.2144152	106.8624295	jawa
way	155957756	pertamina	Pertamina	SPBU Perintis Kemerdekaan	-6.1735769	106.8909013	sumatra
way	159253904	pertamina	SPBU Pertamina	SPBU Pertamina	-6.6128723	106.8128296	jawa
way	159953692	pertamina	SPBU Pertamina 34-15414	SPBU Pertamina 34-15414	-6.3438436	106.7657888	jawa
way	159954860	pertamina	PT Pertamina	SPBU 34-15207	-6.244655	106.6943846	jawa
way	160668329	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5731096	106.7440383	jawa
way	161202373	pertamina	Pertamina	Pertamina	-6.5473505	106.8502249	jawa
way	162651740	pertamina	Pertamina	Pertamina	-6.5688778	106.8558034	jawa
way	163119314	pertamina	Pertamina	SPBU Semeru	-6.5852756	106.7814679	jawa
way	163726406	pertamina	Pertamina	\\N	-6.6579914	106.8576991	jawa
way	164438064	pertamina	SPBU Pertamina	SPBU Pertamina	-6.6400986	106.8350708	jawa
way	164451541	pertamina	Pertamina	\\N	-6.662557	106.8539022	jawa
way	168948947	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5567267	106.7818071	jawa
way	169306681	pertamina	Pertamina	SPBU 34.161.02	-6.5787297	106.8066021	jawa
way	177423764	other	POm Bensin Rest Area Cirendang Kuningan	POm Bensin Rest Area Cirendang Kuningan	-6.9551898	108.4892513	jawa
way	179266753	pertamina	Pertamina	Pertamina	-6.8886545	107.6037642	jawa
way	182333593	pertamina	Pertamina	Pertamina	-6.2501598	106.6249098	jawa
way	182958867	pertamina	Pertamina	34-40318	-6.806146	107.5961636	jawa
way	182985188	pertamina	Pertamina	Pertamina	-6.8116622	107.6348106	jawa
way	183170426	pertamina	Pertamina	Pertamina	-6.9363598	107.6103232	jawa
way	186598274	pertamina	Pertamina	\\N	-7.0981217	106.8823184	jawa
way	187777480	pertamina	Pertamina	SPBU Batutulis 34.16114	-6.620444	106.8071867	jawa
way	188705947	pertamina	Pertamina	\\N	-6.2304601	106.5597806	jawa
way	191318997	pertamina	Pertamina	Pertamina	-7.4216233	109.2444759	jawa
way	191351078	pertamina	SPBU Pertamina	SPBU Pertamina	-6.572599	106.753355	jawa
way	193663805	pertamina	pertamina	SPBU Soreang	-7.0244079	107.530076	jawa
way	194591958	pertamina	SPBU Pelita	SPBU Pelita	-8.1161202	113.2183045	jawa
way	194728221	pertamina	Pertamina	SPBU	-6.4152465	106.7337977	jawa
way	194734532	pertamina	Pertamina	\\N	-6.4347442	106.7345557	jawa
way	194741520	pertamina	SPBU Sukodono	SPBU Sukodono	-8.0999875	113.2341902	jawa
way	196228770	pertamina	SPBU Pasirian	SPBU Pasirian	-8.2216715	113.1202198	jawa
way	196237559	pertamina	SPBU Tempeh	SPBU Tempeh	-8.1807626	113.1818999	jawa
way	198149829	pertamina	SPBU Tekung	SPBU Tekung	-8.143496	113.2361264	jawa
way	198709945	pertamina	SPBU Kedungjajang	SPBU Kedungjajang	-8.0454143	113.2346712	jawa
way	198709946	pertamina	SPBU Klakah	SPBU Klakah	-8.0151662	113.2403141	jawa
way	198709947	pertamina	SPBU Wonorejo	SPBU Wonorejo	-8.0797926	113.2413771	jawa
way	198743846	pertamina	SPBU Yosowilangun	SPBU Yosowilangun	-8.1851979	113.2902718	jawa
way	198989605	other	Pronojiwo	Pronojiwo	-8.2120653	112.9421491	jawa
way	199148616	pertamina	Pertamina	Pertamina	-7.9523201	112.6393321	jawa
way	201054001	pertamina	Pertamina	\\N	-6.1882687	106.5998524	sumatra
way	201199268	pertamina	Pertamina	SPBU 34.166.14	-6.6015253	106.7784616	jawa
way	202969213	pertamina	Pertamina	\\N	-6.5624356	110.6903312	jawa
way	203438268	pertamina	Pertamina	SPBU Pertamina 44.575.09 Tempel	-7.586409	110.7116377	jawa
way	205583011	pertamina	pertamina	SPBU Pertamina Kebonsari	-8.190779	113.693784	jawa
way	211034413	pertamina	Pertamina	Pertamina	-6.1266075	106.8034675	sumatra
way	211050933	pertamina	Pertamina	SPBU 31.107.01	-6.1492294	106.8384995	sumatra
way	213934378	other	pom bensi damalang	pom bensi damalang	-7.7196499	109.0135033	jawa
way	221131713	other	\\N	\\N	-7.1007235	112.1899802	jawa
way	221131714	other	\\N	\\N	-7.095189	112.2912654	jawa
way	222968103	other	\\N	\\N	-7.0307762	109.1430065	jawa
way	222968279	other	\\N	\\N	-7.3509752	109.0724704	jawa
way	224193804	pertamina	spbu	spbu	-6.4749632	106.8297604	jawa
way	224711031	pertamina	SPBU PERTAMINA	SPBU PERTAMINA	-6.4722427	106.8221135	jawa
way	225102020	pertamina	Pertamina	34.165.05	-6.3428256	106.7787781	jawa
way	225103917	pertamina	Pertamina	34.16503	-6.3203386	106.7836745	jawa
way	225760352	pertamina	Pertamina	SPBU Pertamina 43.571.01 Manahan	-7.554123	110.7986693	jawa
way	226234026	pertamina	Pertamina	SPBU34.16414	-6.445144	106.8292191	jawa
way	229484145	pertamina	Pertamina	SPBU Pertamina 54.663.02	-8.0616537	111.7075206	jawa
way	229951699	other	\\N	\\N	-7.3753143	111.219891	jawa
way	230071919	pertamina	Pertamina	Pompa Bensin Non-Subsidi	-6.2005287	106.6405715	jawa
way	234735789	pertamina	Pertamina	SPBU Pertamina 44.556.02 Wates Kota	-7.8670818	110.1498573	jawa
way	234748666	pertamina	Pertamina	SPBU Pertamina 44.556.08 Glagah	-7.9058145	110.0789307	jawa
way	240896797	pertamina	Pertamina	Pertamina	-6.2217088	106.6203841	jawa
way	242222687	pertamina	SPBU Pangalengan	SPBU Pangalengan	-7.1758628	107.5739258	jawa
way	242223578	pertamina	SPBU Banjaran	SPBU Banjaran	-7.047192	107.5816569	jawa
way	244660561	pertamina	Pertamina	\\N	-7.7739979	110.4103711	jawa
way	249655080	pertamina	Pertamina	SPBU Pertamina 41.551.01 Baciro	-7.7904458	110.3799083	jawa
way	253638579	other	\\N	\\N	-7.2663195	110.3832733	jawa
way	255193139	pertamina	SPBU Bejen	SPBU Bejen	-7.6031829	110.9707474	jawa
way	255344325	pertamina	Pertamina	\\N	-8.3362487	114.1487145	jawa
way	255512295	pertamina	SPBU Jatisrono	SPBU Jatisrono	-7.8294901	111.1607736	jawa
way	256262340	pertamina	Pertamina	SPBU	-6.5677315	106.804517	jawa
way	258738707	pertamina	VIVO	SPBU VIVO Jatibening	-6.2652482	106.9504496	jawa
way	261642692	other	\\N	\\N	-7.5340667	112.0726427	jawa
way	264985164	other	\\N	\\N	-7.0135052	109.1420535	jawa
way	266184008	other	Kaligadung	Kaligadung	-7.2156472	109.0186253	jawa
way	270038248	pertamina	Pertamina	SPBU Pertamina 41.552.01 Laksda Adisucipto	-7.7829983	110.4080517	jawa
way	270039324	pertamina	SPBU	SPBU	-7.2707855	110.0910597	jawa
way	277545721	pertamina	Pertamina	\\N	-7.4176436	109.3398158	jawa
way	281797216	pertamina	Pertamina	Pertamina	-7.6870257	112.0799602	jawa
way	285018918	pertamina	Pertamina	Pertamina	-6.891756	107.5629229	jawa
way	287709141	other	\\N	\\N	-7.4019002	109.3482721	jawa
way	290388487	pertamina	Pertamina	\\N	-7.2005827	110.3919969	jawa
way	291853454	pertamina	Pertamina	Pertamina	-6.3741391	106.8329015	jawa
way	296147183	pertamina	Pertamina	SPBU Lingkar Nagrek	-7.0292461	107.8836734	jawa
way	299058088	pertamina	Pertamina	Pertamina	-6.890816	107.6165945	jawa
way	300224202	pertamina	Pertamina	SPBU Sidomoro	-7.1725696	112.6533888	jawa
way	300224670	pertamina	Pertamina	Pertamina 54.611.15 Veteran	-7.187757	112.6468262	jawa
way	300729535	shell	Shell	\\N	-6.3811187	106.8307614	jawa
way	301068114	shell	Shell	Shell	-6.36499	106.833547	jawa
way	303408710	pertamina	Pertamina	Pertamina	-6.336467	106.7400196	jawa
way	304103665	other	SPBBE	SPBBE	-7.2007132	109.0213691	jawa
way	304107373	other	Jatisawit	Jatisawit	-7.2653854	109.0126672	jawa
way	304196221	pertamina	Pertamina	Pom Bensin Arjawinangun	-6.6519063	108.4095363	jawa
way	304379083	pertamina	PERTAMINA	SPBU MAYONG	-6.7454645	110.7524489	jawa
way	304379729	pertamina	PERTAMINA	SPBU PULODARAT	-6.6927192	110.7145438	jawa
way	304380007	pertamina	PERTAMINA	SPBU TROSO	-6.6899751	110.7032814	jawa
way	304469699	pertamina	Pertamina	SPBU Hiu Kalibenda	-7.3923633	109.7321122	jawa
way	307261244	pertamina	SPBU Miagan	SPBU Miagan	-7.5659988	112.3609833	jawa
way	307261246	pertamina	SPBU Miagan	SPBU Miagan	-7.5663274	112.361013	jawa
way	307278964	pertamina	PT Pertamina	\\N	-6.2337414	106.9656739	jawa
way	307280777	other	\\N	\\N	-7.036835	113.5471367	jawa
way	307318980	pertamina	Pertamina	SPBU Pelem	-7.7622687	112.1712378	jawa
way	307391099	pertamina	Pertamina	SPBU Labruk Lor	-8.139532	113.2086665	jawa
way	308839506	pertamina	Pertamina	SPBU Pertamina 44.576.08 Brumbung	-7.7962199	110.9062163	jawa
way	311504049	pertamina	Pertamina	SPBU Pertamina 54.651.56	-7.8718923	112.3771244	jawa
way	319060478	pertamina	Pertamina	SPBU Pertamina 44.555.24 Pokoh	-7.6688343	110.4287831	jawa
way	319318385	pertamina	Pertamina	Pertamina	-6.2421125	107.0150062	jawa
way	319318386	pertamina	Pertamina	Pertamina 34-17135	-6.2357182	107.0073898	jawa
way	319322528	pertamina	PT Pertamina	SPBU 34-17106	-6.2380156	107.0009924	jawa
way	319326270	pertamina	Pertamina	Pertamina	-6.2498179	107.0000144	jawa
way	321005288	pertamina	Pertamina	Pertamina	-6.8935305	107.5860977	jawa
way	321229426	pertamina	Pertamina	SPBU Pertamina 44.552.07 Ambarketawang	-7.8028754	110.3133033	jawa
way	323377552	pertamina	Pertamina	SPBU Sampang	-7.1823534	113.234557	jawa
way	324038875	pertamina	Pertamina	SPBU Pertamina 54.661.17	-8.0350873	112.3414139	jawa
way	324283686	pertamina	Pertamina	SPBU 34.161.21	-6.6118112	106.8017696	jawa
way	324379973	pertamina	Pertamina	\\N	-7.7231216	109.9068227	jawa
way	324950922	pertamina	Pertamina	Pom Bensin 54.651.40 Talangagung	-8.126215	112.5595852	jawa
way	325122560	pertamina	Pertamina	Pertamina	-7.9863144	112.6267492	jawa
way	326488348	pertamina	Pertamina	SPBU Pertamina 44.571.01 Pabelan	-7.5611227	110.76468	jawa
way	327001673	pertamina	Pertamina	SPBU Pertamina 44.571.18 Gembongan	-7.5564282	110.7511865	jawa
way	327001674	other	\\N	\\N	-7.5537692	110.7444681	jawa
way	327003227	pertamina	Pertamina	SPBU Pertamina 44.575.11 Kopen	-7.559256	110.7484424	jawa
way	328128217	pertamina	SPBU Pangelen	SPBU Pangelen	-7.1354424	113.2383741	jawa
way	328185751	pertamina	SPBU Panarukan	SPBU Panarukan	-7.6994971	113.9499917	jawa
way	328283119	pertamina	SPBU Patokan	SPBU Patokan	-7.7444649	114.0092719	jawa
way	328300495	pertamina	SPBU Banyuanyar	SPBU Banyuanyar	-7.2118235	113.2645682	jawa
way	328303345	other	\\N	\\N	-7.1823402	113.2345755	jawa
way	328525770	pertamina	SPBU Kesambirampak	SPBU Kesambirampak	-7.6926373	114.0526703	jawa
way	328586786	pertamina	Pertamina	Pertamina	-7.2176301	113.3102069	jawa
way	328631558	pertamina	SPBU Bancelok	SPBU Bancelok	-7.1354324	113.1655123	jawa
way	328808644	pertamina	SPBU Panarukan	SPBU Panarukan	-7.6995312	113.9499695	jawa
way	328901543	pertamina	SPBU Pasir Putih	SPBU Pasir Putih	-7.6860136	113.8458912	jawa
way	328984959	pertamina	SPBU	SPBU	-7.2430827	110.4350668	jawa
way	329257083	pertamina	Pertamina	SPBU Pertamina 44.571.21 Manahan	-7.554851	110.8115656	jawa
way	329316048	pertamina	Pertamina	SPBU Pertamina 44.575.15 Semanggi	-7.5860089	110.8340654	jawa
way	329406617	pertamina	SPBU MUNJUNGAN	SPBU MUNJUNGAN	-8.3085173	111.5748913	jawa
way	329469971	pertamina	SPBU Munjungan	SPBU Munjungan	-8.3085334	111.5747978	jawa
way	329616694	pertamina	Pertamina	SPBU Pertamina 54.663.07	-8.2511746	111.4602402	jawa
way	330176516	pertamina	Pertamina	SPBU Pertamina 54.663.06	-8.282589	111.723645	jawa
way	332001467	pertamina	Pertamina	SPBU Pertamina 44.575.16 Ngabeyan	-7.5449058	110.7440976	jawa
way	332078276	pertamina	SPBU Semen	SPBU Semen	-7.6215069	110.3118792	jawa
way	332574372	pertamina	SPBU Pertamina	SPBU Pertamina	-6.3795131	106.7460182	jawa
way	333931786	pertamina	Pertamina	SPBU 34-17124	-6.2611408	107.0007425	jawa
way	334877460	pertamina	Pertamina	SPBU 34-17126	-6.269459	107.0428504	jawa
way	335114287	pertamina	Pertamina	SPBU Pertamina 54.651.48	-7.7819983	112.310392	jawa
way	335712518	pertamina	Pertamina	SPBU 34-12806	-6.2384592	106.8442488	jawa
way	336892698	pertamina	Pertamina	SPBU Pertamina Jatinegara	-7.0609873	109.2403032	jawa
way	336892700	pertamina	Pertamina	SPBU Pertamina Jatinegara	-7.0607982	109.2402565	jawa
way	337725603	pertamina	POM Bensin PERTAMINA	POM Bensin PERTAMINA	-6.2228825	106.7050629	jawa
way	339173624	pertamina	SPBU 44.524.01	SPBU 44.524.01	-7.0608405	109.2403236	jawa
way	339671044	pertamina	Pertamina	Pertamina	-6.253108	107.0232175	jawa
way	340168690	pertamina	Pertamina	Pertamina	-6.2581591	106.9940432	jawa
way	341945867	pertamina	VIVO	SPBU VIVO Bintaro Sektor 9	-6.2730828	106.7124991	jawa
way	341945869	pertamina	VIVO	SPBU VIVO Modernland	-6.19471	106.6355616	sumatra
way	341945870	pertamina	VIVO	SPBU VIVO Daan Mogot	-6.165703	106.7828194	sumatra
way	342033408	shell	Shell	Shell Ahmad Yani-1 BKS	-6.2485694	106.9927278	jawa
way	342033411	shell	Shell	Shell	-6.3287653	107.1290361	jawa
way	345613943	pertamina	Pertamina	SPBU Pertamina 44.556.09 Bantengan Galur	-7.9403376	110.2281065	jawa
way	346116790	pertamina	SPBU 54-63111	SPBU 54-63111	-7.6241093	111.5344608	jawa
way	346589866	pertamina	Pertamina	Pertamina	-7.5010253	110.9164413	jawa
way	346589867	other	\\N	\\N	-7.5458506	110.8858244	jawa
way	347402431	other	\\N	\\N	-7.6057405	110.7911697	jawa
way	347403870	pertamina	Pertamina	SPBU Pertamina 41.575.01 Solo Baru	-7.6005536	110.8137157	jawa
way	347411723	pertamina	Pertamina	SPBU Pertamina 44.575.15 Puri Gading	-7.5955894	110.8216578	jawa
way	350241422	pertamina	Pertamina	SPBU Pertamina 44.575.27 Laweyan	-7.571104	110.8011627	jawa
way	352253856	pertamina	Pertamina	SPBU Pertamina 44.552.09 Kentungan	-7.7558336	110.3824829	jawa
way	353187031	pertamina	Pertamina	SPBU Pertamina 44.573.02 Sawit	-7.5709362	110.7191897	jawa
way	354493260	pertamina	Pertamina	SPBU Pertamina 54.641.12	-7.7463119	112.1994097	jawa
way	355376240	pertamina	Pertamina	SPBU 54.661.20 Bendo–Ponggok	-8.0827729	112.1212673	jawa
way	355584085	pertamina	Pertamina	SPBU Pertamina 54.641.31	-7.7804487	112.1231223	jawa
way	356386390	pertamina	Pertamina	Pertamina	-6.9374582	107.693917	jawa
way	356386391	pertamina	Pertamina	SPBU Pertamina 31.171.01	-6.2509672	106.9921626	jawa
way	356386393	pertamina	Pertamina	SPBU 31.161.13	-6.5740261	106.7883352	jawa
way	356696356	pertamina	Pertamina	SPBU 34-17541	-6.2761302	107.0620363	jawa
way	356696357	pertamina	Pertamina	Pertamina	-6.292511	107.0794058	jawa
way	356696358	pertamina	Pertamina	Pertamina	-6.3301687	107.1297291	jawa
way	356696359	pertamina	Pertamina	SPBU 31.413.01	-6.3924589	107.4249429	jawa
way	357237062	pertamina	Pertamina	Pertamina	-6.2389304	106.6591725	jawa
way	357578930	pertamina	Pertamina	SPBU Pertamina 54.641.24	-7.8075897	112.2422499	jawa
way	357732711	pertamina	Pertamina	Pertamina	-6.2591285	107.0145247	jawa
way	357766923	pertamina	Pertamina	Pertamina	-6.3678476	107.3610329	jawa
way	359275173	pertamina	Pertamina	SPBU 34-17114	-6.2619223	106.9987383	jawa
way	359275174	pertamina	Pertamina	SPBU 34-17102	-6.2606184	106.9952648	jawa
way	359275175	pertamina	Pertamina	Pertamina	-6.256913	106.9884226	jawa
way	359277856	pertamina	Pertamina	SPBU	-6.2860023	107.1589882	jawa
way	359277857	pertamina	Pertamina	SPBU	-6.2769466	107.2753259	jawa
way	359277858	pertamina	Pertamina	SPBU 34-41354	-6.2856945	107.3082309	jawa
way	361647471	pertamina	Pertamina	Pertamina 34-17105	-6.2789544	106.9728138	jawa
way	361647585	pertamina	Pertamina	Pertamina 34.174.17	-6.2874806	106.9683299	jawa
way	361673244	pertamina	Pertamina	Pertamina	-6.244676	106.6944013	jawa
way	361674151	pertamina	Pertamina	SPBU 34-15117	-6.2321938	106.7022493	jawa
way	362198230	pertamina	Pertamina	SPBU Pertamina 44.552.10 Janti	-7.7836022	110.4065692	jawa
way	363096059	pertamina	Pertamina	Pertamina 34 171 43	-6.2593391	106.9592399	jawa
way	363135726	pertamina	Pertamina	SPBU Pertamina 44.552.19 Kasam Babarsari	-7.7765073	110.4149436	jawa
way	363608565	pertamina	Pertamina	SPBU Pertamina 44.555.29 Gito Gati	-7.719747	110.3753937	jawa
way	363648757	pertamina	Pertamina	Pertamina 34.17145	-6.2707806	106.9936983	jawa
way	363786141	pertamina	Pertamina	Pertamina	-8.1788097	113.6794559	jawa
way	363879121	pertamina	Pertamina	SPBU Pertamina 44.555.22 Sidomoyo	-7.75817	110.3202304	jawa
way	364888791	pertamina	Pertamina	SPBU Pertamina 44.555.15 Kalasan	-7.7746413	110.462746	jawa
way	365233829	other	\\N	\\N	-7.5912584	110.2831054	jawa
way	365448548	pertamina	Pertamina	SPBU Pertamina 44.555.23 Gendol	-7.6839505	110.2986237	jawa
way	366421535	pertamina	SPBU 44.555.09	SPBU 44.555.09	-7.7279826	110.3803149	jawa
way	368853914	pertamina	Pertamina	SPBU Pertamina 44.555.12 Kembang Arum	-7.6525265	110.3735372	jawa
way	369615575	pertamina	Pertamina	Pertamina	-6.8727346	107.553246	jawa
way	371168602	pertamina	Pertamina	SPBU	-6.2428374	107.0277401	jawa
way	371681790	pertamina	Pertamina	SPBU 44.534.04 Karangtengah	-7.2028338	109.9024369	jawa
way	374262114	pertamina	SPBU 44.531.29 Rawalo	SPBU 44.531.29 Rawalo	-7.5370769	109.1767861	jawa
way	374532844	pertamina	Pertamina	SPBU Pertamina 54.628.05 Tamansari	-7.9211259	113.8271104	jawa
way	376322812	pertamina	Pertamina	SPBU Pertamina 34-41351	-6.3490885	107.3777175	jawa
way	377097013	bp	BP	BP	-6.3041334	106.8434821	jawa
way	383052743	pertamina	Pertamina	Pertamina	-6.9396348	107.6625098	jawa
way	387298959	other	\\N	\\N	-6.3863558	106.8678505	jawa
way	387298960	pertamina	Pertamina	SPBU 34-16920	-6.4426711	106.8539077	jawa
way	387787702	pertamina	PT Pertamina	SPBE Pertamina	-6.3246875	107.0214258	jawa
way	391859740	pertamina	SPBU Pertamina	SPBU Pertamina	-6.8980911	107.5970165	jawa
way	391873822	pertamina	Pertamina	SPBU 3442113	-6.1202478	106.1744476	sumatra
way	392012454	other	\\N	\\N	-6.6903728	106.8458839	jawa
way	395230514	pertamina	Pertamina	Pertamina	-6.3162777	106.7414951	jawa
way	396469031	pertamina	Pertamina	SPBU Pertamina 44.551.17 Brontokusuman	-7.815762	110.37424	jawa
way	396844570	pertamina	SPBU SPDN	SPBU SPDN	-6.1595724	107.4908277	jawa
way	398069068	pertamina	Pertamina	Pertamina	-6.2954045	106.6802124	jawa
way	398181511	pertamina	Pertamina	Pertamina	-6.2903607	106.6653312	jawa
way	398181535	shell	Shell	Shell	-6.2914294	106.6663873	jawa
way	398236937	pertamina	Pertamina	Pertamina	-6.4361138	106.8933383	jawa
way	398236939	shell	Shell	Shell	-6.4349458	106.8935198	jawa
way	400316130	pertamina	Pertamina	Pertamina	-6.3485189	107.2058052	jawa
way	400316132	pertamina	Pertamina	Pertamina	-6.3474474	107.2045382	jawa
way	403421654	bp	BP	BP	-7.9682265	112.6385236	jawa
way	406533170	pertamina	Pertamina	SPBU Pertamina 44.558.12 Duwet	-7.9980784	110.5924435	jawa
way	416534322	pertamina	Pertamina	SPBU Pertamina 44.575.19 Sidan	-7.6129888	110.8891432	jawa
way	416535175	pertamina	Pertamina	SPBU Pertamina 44.575.05 Wirun	-7.6026408	110.8644231	jawa
way	426568894	pertamina	Pertamina	Pertamina	-6.919876	107.6119467	jawa
way	437773788	pertamina	Pertamina	Pertamina	-6.2949781	106.7139418	jawa
way	437891123	pertamina	Pertamina	Pertamina	-6.3932624	105.856873	jawa
way	437891125	pertamina	Pertamina	Pertamina	-6.3834465	105.836151	jawa
way	437891131	pertamina	Pertamina	Pertamina	-6.395133	105.9937964	jawa
way	438981327	pertamina	Pertamina	Pertamina	-6.1320219	106.20544	sumatra
way	438981485	pertamina	Pertamina	Pertamina	-6.1325234	106.2037151	sumatra
way	441543547	pertamina	Pertamina	Pertamina	-6.2139988	106.6782072	jawa
way	443160921	pertamina	Pertamina	Pertamina	-6.2143931	106.6815907	jawa
way	445622279	pertamina	SPBU Rest Area 45	SPBU Rest Area 45	-6.1857248	106.4077825	sumatra
way	448819974	pertamina	Pertamina	SPBU Pertamina 44.574.18 Daleman	-7.6215533	110.7644349	jawa
way	454108291	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5689851	106.7663977	jawa
way	460445535	pertamina	Pertamina	Pertamina 54.601.73	-7.2369421	112.6186831	jawa
way	466322864	pertamina	Pertamina	SPBU Pertamina 44.574.03 Jonggrangan	-7.6900114	110.6180601	jawa
way	469373373	other	POM TNI	POM TNI	-7.3323652	110.5097622	jawa
way	471675180	pertamina	Pertamina	SPBU Pertamina 54.641.11	-7.8411806	112.0093104	jawa
way	472066271	pertamina	SPBU Walikukun	SPBU Walikukun	-7.4104573	111.2251319	jawa
way	472076070	pertamina	Pertamina	Pertamina	-7.395251	112.7271704	jawa
way	472485112	pertamina	SPBU	SPBU	-6.467461	107.8100634	jawa
way	472485114	pertamina	SPBU 34 - 41226	SPBU 34 - 41226	-6.5202092	107.7932798	jawa
way	476604827	other	Pom Bensin Dersalam	Pom Bensin Dersalam	-6.8040212	110.8692306	jawa
way	476645877	other	POM Ngembal Kulon	POM Ngembal Kulon	-6.8145505	110.8793897	jawa
way	480116161	pertamina	Pertamina	Pertamina	-6.243504	106.626534	jawa
way	480165634	pertamina	Pertamina	Pertamina	-6.3729894	106.8720014	jawa
way	480320332	pertamina	Pertamina	Pertamina	-6.2504841	106.6499254	jawa
way	480574343	pertamina	Pertamina	Pertamina	-6.5779843	106.7590831	jawa
way	480994691	pertamina	Pertamina	Pertamina	-6.1353901	107.0559502	jawa
way	481723251	pertamina	Pertamina	Pertamina	-6.1931408	106.8762007	sumatra
way	481733399	pertamina	Pertamina	Pertamina	-6.2733512	106.8188454	jawa
way	482144265	pertamina	Pertamina	Pertamina	-6.2667961	106.8156886	jawa
way	482203973	pertamina	Pertamina	Pertamina	-6.1981028	106.8548421	sumatra
way	482341392	pertamina	Pertamina	Pertamina	-6.4231441	106.8565881	jawa
way	482343668	pertamina	Pertamina	Pertamina	-6.2026135	106.857364	jawa
way	482378860	other	Pom Bensin Batu Jaya	Pom Bensin Batu Jaya	-6.0716157	107.1566658	jawa
way	482404309	pertamina	Pertamina	SPBU 34.137.11	-6.3007049	106.8696779	jawa
way	483399557	pertamina	Pertamina	SPBU 34 13414	-6.2265168	106.8797333	jawa
way	483640957	pertamina	Pertamina	SPBU 34.13420	-6.2389729	106.8883577	jawa
way	483644518	pertamina	Pertamina	Pertamina	-6.1923026	106.865025	sumatra
way	484116945	other	SPBG PGN Pondok Ungu	SPBG PGN Pondok Ungu	-6.2009101	106.9755423	jawa
way	484685080	pertamina	Pertamina	SBPU 34-13602 Pertamina	-6.2521274	106.8646491	jawa
way	484848157	pertamina	Pertamina	Pertamina	-6.1966512	106.874851	sumatra
way	485030839	pertamina	SPBU Sengguruh	SPBU Sengguruh	-8.1663085	112.5507684	jawa
way	486392468	pertamina	Pertamina	Pertamina	-6.176069	106.8677142	sumatra
way	487275707	other	\\N	\\N	-6.3840585	106.829752	jawa
way	487564147	pertamina	Pertamina	34.13201	-6.213745	106.8933192	jawa
way	487578120	pertamina	Pertamina	Pertamina	-6.2478348	106.8268616	jawa
way	487912144	pertamina	Pertamina	SPBU Bapangan	-6.6036239	110.6786491	jawa
way	487956973	pertamina	Pertamina	Pertamina	-6.8415979	110.8162661	jawa
way	488142682	pertamina	SPBU Limo Raya	SPBU Limo Raya	-6.3664265	106.775354	jawa
way	488197085	pertamina	Pertamina	SPBU 34-12804	-6.2359091	106.8446464	jawa
way	489251353	pertamina	Pertamina	SPBU 34.127.01	-6.2615366	106.8289608	jawa
way	489499706	pertamina	Pertamina	Pertamina 34-13502	-6.2895079	106.8557552	jawa
way	490789034	pertamina	Pertamina	Pertamina	-6.4064726	106.8907836	jawa
way	491439226	pertamina	SPBU Pertamina	SPBU Pertamina	-6.560093	106.7862985	jawa
way	491491484	bp	BP	BP	-6.1776781	106.7289656	sumatra
way	492518310	pertamina	Pertamina	SPBU 34-13206	-6.1938056	106.8871639	sumatra
way	493580248	pertamina	Pertamina	SPBU Metro Jaya	-6.2906123	106.8716901	jawa
way	493711385	pertamina	Pertamina	Pertamina	-6.1842724	106.7304093	sumatra
way	494565072	pertamina	Pertamina	Pertamina	-6.2172111	106.6016	jawa
way	497530902	pertamina	Pertamina	SPBU 34-13209	-6.1925472	106.8824692	sumatra
way	497530908	shell	Shell	Shell Pemuda-2 EAST JKT	-6.1926329	106.8818836	sumatra
way	498480777	other	SPB Dinas Mabes POLRI	SPB Dinas Mabes POLRI	-6.2381474	106.8008409	jawa
way	498483662	pertamina	Pertamina	SPBT 32-12402	-6.1556972	106.8867853	sumatra
way	498997723	pertamina	Pertamina	Pertamina	-6.2856332	106.8551993	jawa
way	499264767	pertamina	Pertamina	Pertamina	-6.2211238	106.9646148	jawa
way	499879153	pertamina	Pertamina	SPBU 3442407	-5.9913587	106.0285707	sumatra
way	500212043	pertamina	Pertamina	SPBU 3442412	-6.0288528	105.96341	sumatra
way	500401095	pertamina	Pertamina	SPBU 3442410	-5.9651358	106.0031945	sumatra
way	500562099	pertamina	Pertamina	SPBU 3442408	-5.9498789	106.0023604	sumatra
way	500581704	pertamina	Pertamina	SPBU 3442409	-5.9586256	106.0060878	sumatra
way	500720023	pertamina	Pertamina	Pertamina	-6.2158218	106.619281	jawa
way	500858324	pertamina	Pertamina	SPBU 34-10506	-6.175109	106.8523212	sumatra
way	500909584	pertamina	Pertamina	Pertamina 34-17139	-6.2214344	107.0181711	jawa
way	501123479	pertamina	SPBU Tigaraksa	SPBU Tigaraksa	-6.2419724	106.4722263	jawa
way	501627827	pertamina	Pertamina	SPBU 34-13911	-6.2031039	106.9469667	jawa
way	501946101	pertamina	Pertamina	Pertamina 34-17148	-6.1892753	107.0066819	jawa
way	501968331	pertamina	Pertamina	Pertamina	-6.1604828	106.8352279	sumatra
way	501977770	pertamina	Pertamina	\\N	-6.2871548	106.8259317	jawa
way	502243252	pertamina	Pertamina	34.10701	-6.1616693	106.8259837	sumatra
way	502248002	pertamina	Pertamina	SPBU 34-13415	-6.2270336	106.9363929	jawa
way	502285319	other	PT Kereta Api Indonesia	\\N	-6.1931612	106.971581	sumatra
way	502518061	pertamina	Pertamina	Pertamina 34.13.603	-6.2519157	106.9088874	jawa
way	502518066	shell	Shell	Shell Jatiwaringin-1 East JKT	-6.2482662	106.9073924	jawa
way	502713964	pertamina	Pertamina	Pertamina	-6.2155196	106.5631731	jawa
way	502885785	other	\\N	\\N	-6.3216923	106.6411385	jawa
way	503041672	pertamina	Pertamina	34.10101	-6.1607364	106.8155931	sumatra
way	503319669	pertamina	Pertamina	Pertamina	-6.3750663	106.8319754	jawa
way	503659064	other	Pommini	Pommini	-6.2688684	106.8775167	jawa
way	503700884	pertamina	Pertamina	Pertamina 34. 136.05	-6.251464	106.8810481	jawa
way	504335318	pertamina	Pertamina	SPBU Pertamina 34-13801	-6.2912673	106.8883788	jawa
way	504496462	pertamina	Pertamina	Pertamina SPBU 34-16415	-6.39229	106.8426506	jawa
way	504672784	pertamina	Pertamina	Pertamina	-6.2753222	106.912987	jawa
way	504984105	pertamina	Pertamina	Pertamina	-6.2472104	106.9114225	jawa
way	505454831	pertamina	Pertamina	SPBU 34-13903	-6.2045134	106.9348344	jawa
way	505766664	pertamina	Pertamina	Pertamina	-6.2722441	106.7979272	jawa
way	505772092	pertamina	Pertamina	Pertamina	-6.1482453	106.8308937	sumatra
way	506543152	pertamina	Pertamina	Pertamina 34.175.32	-6.1760996	107.1781579	jawa
way	506584992	pertamina	Pertamina	SPBU 34.106.03	-6.1502112	106.8353472	sumatra
way	506589445	pertamina	Pertamina	Pertamina	-7.1111133	110.3975832	jawa
way	507151844	shell	Shell	Shell	-6.5762649	106.8079994	jawa
way	508053596	pertamina	Pertamina	Pertamina	-7.3654585	112.7285256	jawa
way	508098137	pertamina	Pertamina	Pertamina	-7.3719813	112.7286778	jawa
way	508324595	pertamina	Pertamina	SPBU 31.139.01	-6.182092	106.9343105	sumatra
way	509398409	pertamina	Pertamina	Pertamina	-7.2481633	112.5609735	jawa
way	509813957	pertamina	Pertamina	SPBU 34-13418	-6.2463667	106.9426074	jawa
way	509834711	pertamina	Pertamina	Pertamina	-6.2958405	106.8252148	jawa
way	510153301	pertamina	Pertamina	\\N	-6.2383953	106.8990839	jawa
way	510336242	pertamina	Pertamina	Pertamina	-6.3026525	106.8584041	jawa
way	510429613	pertamina	Pertamina	SPBU 34-13804	-6.3089371	106.8689931	jawa
way	510717550	pertamina	SPBU 34.45501	SPBU 34.45501	-7.0039022	108.4497706	jawa
way	510968227	pertamina	Pertamina	Pertamina	-6.292546	106.8115294	jawa
way	511229681	pertamina	Pertamina	Pertamina	-6.2811443	106.8192931	jawa
way	511511473	pertamina	Pertamina	Pertamina	-7.3526915	112.7184527	jawa
way	511592052	pertamina	Pertamina	SPBU 34-13412	-6.2292865	106.8922996	jawa
way	511671692	pertamina	Pertamina	SPBU 34-14209	-6.1343989	106.9161658	sumatra
way	512827516	pertamina	Pertamina	Pertamina	-6.2974438	106.9220711	jawa
way	513474352	shell	Shell	Shell	-6.1889601	106.7715457	sumatra
way	514372056	pertamina	Pertamina	SPBU 34-14103	-6.1312112	106.9147866	sumatra
way	514534381	pertamina	Pertamina	SPBU 44-56205	-7.2839588	110.1113262	jawa
way	514535271	pertamina	SPBU 44-56208	SPBU 44-56208	-7.3058148	110.1489926	jawa
way	515411849	pertamina	Pertamina	SPBU Pertamina 34-13802	-6.2896263	106.8965893	jawa
way	515703457	pertamina	Pertamina	Pertamina	-6.9382568	107.6168083	jawa
way	515704095	pertamina	Pertamina	Pertamina	-6.9376946	107.6178229	jawa
way	517575935	other	POM BENSIN TENGAH TANI	POM BENSIN TENGAH TANI	-6.7087088	108.5210956	jawa
way	517799651	pertamina	Pertamina	SPBU Pertamina 44.551.09 Semaki	-7.801757	110.3857997	jawa
way	518507113	other	Pom Bensin	Pom Bensin	-6.1901512	107.0183439	jawa
way	518511288	other	Pom Bensin Kebalen	Pom Bensin Kebalen	-6.183146	107.0394756	jawa
way	519199930	pertamina	Pertamina	SPBU 34-134-22	-6.2478387	106.9300199	jawa
way	519539714	pertamina	Pertamina	SPBU 34-13406	-6.2337493	106.9459362	jawa
way	519548001	pertamina	Pertamina	SPBU 34.14207	-6.128962	106.9079388	sumatra
way	519879173	pertamina	Pertamina	Pertamina	-6.2374223	106.9225981	jawa
way	520234510	pertamina	Pertamina	SPBU 34-13419	-6.2419994	106.9203316	jawa
way	520277806	pertamina	Pertamina	SPBU 34.14413	-6.1395268	106.8473036	sumatra
way	521987257	bp	BP	BP	-6.1481123	106.8601777	sumatra
way	523552680	pertamina	Pertamina	SPBU 3442411	-6.010688	106.0183936	sumatra
way	523948948	pertamina	Pertamina	SPBU 3442405	-6.0315608	106.0786025	sumatra
way	525493971	pertamina	Pertamina	SPBU 34 12512	-6.2904815	106.8235293	jawa
way	525515314	pertamina	Pertamina	Pertamina	-6.1255504	106.8013368	sumatra
way	527415739	shell	Shell	Shell	-6.1786839	106.7290679	sumatra
way	527775467	pertamina	Depot Gas Pertamina	Depot Gas Pertamina	-6.1452939	106.7298876	sumatra
way	528076648	pertamina	Pertamina	Pertamina	-6.1341952	106.8345826	sumatra
way	528623275	pertamina	SPBU	SPBU	-7.6022575	110.2996951	jawa
way	528623281	pertamina	SPBU	SPBU	-7.6024455	110.2996126	jawa
way	529360864	pertamina	Pertamina	SPBU 3414407	-6.1312203	106.7918478	sumatra
way	529368061	shell	Shell	Shell Antasari-1 SOUTH JKT	-6.2564436	106.8083652	jawa
way	529381555	pertamina	Pertamina	SPBU 34. 12401	-6.2585724	106.7966496	jawa
way	529396756	pertamina	Pertamina	Pertamina	-6.2442538	106.7880203	jawa
way	529632557	pertamina	Pertamina	SPBU 3442403 CIGADING	-6.0161838	105.9750164	sumatra
way	530026833	pertamina	Pertamina	SPBU Pertamina 44.551.02 Ketandan	-7.811003	110.4046261	jawa
way	530707734	pertamina	Pertamina	SPBU	-6.8286085	107.1252353	jawa
way	530717227	pertamina	Pertamina	Pertamina	-7.9102386	112.6579078	jawa
way	530750267	pertamina	Pertamina	SPBU Pertamina 45.595.18 Bandungrejo	-7.0239219	110.5071431	jawa
way	530836002	pertamina	Pertamina	SPBU Pertamina 44.558.05 Kepek	-7.9591121	110.5940171	jawa
way	530968370	pertamina	Pertamina	SPBU Pertamina 44.557.13 Kretek	-7.9922274	110.3156062	jawa
way	531005395	pertamina	Pertamina	SPBU Pertamina 44.555.21 Bokoharjo	-7.7884247	110.4821835	jawa
way	531181647	pertamina	Pertamina	Pertamina	-6.3075626	106.8778764	jawa
way	531940147	pertamina	Pertamina	Pertamina	-6.2678486	106.7065486	jawa
way	533301061	pertamina	Pertamina	SPBU Pertamina 44.555.26 Seyegan	-7.723725	110.3079645	jawa
way	533714402	pertamina	Pertamina	SPBU Pertamina 44.555.25 Selomartani	-7.7217912	110.4727415	jawa
way	533720123	pertamina	Pertamina	SPBU Pertamina 44.555.14 Kalitirto	-7.7791596	110.4557344	jawa
way	534438836	pertamina	Pertamina	Pertamina 34-15416	-6.301573	106.7514149	jawa
way	534456010	pertamina	Pertamina	SPBU Pertamina 44.571.11 Banyuagung	-7.5417958	110.8158136	jawa
way	534574360	pertamina	Pertamina	SPBU Pertamina 44.571.08 Tipes	-7.5803237	110.8152523	jawa
way	534768644	other	\\N	\\N	-6.3406048	108.0942467	jawa
way	535218059	pertamina	Pertamina	SPBU Pertamina 44.557.07 Jalimbar	-7.8605745	110.37427	jawa
way	535417778	pertamina	Pertamina	Pertamina	-6.7061363	108.2247012	jawa
way	536023266	pertamina	Pertamina	SPBU Pertamina 44.571.15 Kadipiro	-7.5312263	110.819153	jawa
way	537079758	pertamina	Pertamina	Pertamina	-6.2725037	106.7493003	jawa
way	537135043	pertamina	Pertamina	Pertamina	-6.2809515	106.7463594	jawa
way	537339656	pertamina	SPBU Pertamina 34-45111	SPBU Pertamina 34-45111	-6.6924274	108.5508444	jawa
way	537904220	pertamina	Pertamina	SPBU Pertamina 44.555.05 Prujakan	-7.736097	110.3944927	jawa
way	538641347	pertamina	Pertamina	Pertamina	-6.2622881	106.8085376	jawa
way	540101188	pertamina	Pertamina	Pertamina	-6.7136748	108.2430145	jawa
way	540676149	pertamina	SPBU Karangampel	SPBU Karangampel	-6.4758524	108.4604413	jawa
way	545217888	pertamina	SPBU 34-1747	SPBU 34-1747	-6.26528	107.0087757	jawa
way	545217896	pertamina	Pertamina	Pertamina	-6.2689165	107.0014355	jawa
way	551018585	pertamina	Pertamina	SPBU Pertamina 44.556.12 Dekso	-7.7153379	110.2268078	jawa
way	552421290	shell	Shell	Shell	-6.2908279	106.6392792	jawa
way	552506113	pertamina	Pertamina	SPBU Pertamina 44.555.30 Cebongan	-7.732237	110.3255251	jawa
way	553658407	pertamina	Pertamina	Pertamina 44 541 01	-7.7494432	110.0027353	jawa
way	555428846	pertamina	Pertamina	SPBU Pertamina 41.552.02 Tegalrejo	-7.7856336	110.3527336	jawa
way	556020160	pertamina	Pertamina	SPBU Pertamina 44.555.28 Getas	-7.734418	110.3396355	jawa
way	558715851	pertamina	Pertamina	Pertamina	-6.2679767	107.0167581	jawa
way	564399387	other	Petrolimex	Petrolimex	-8.1771136	114.7492069	jawa
way	568819139	pertamina	Pertamina	Pertamina Sekaran	-7.0560142	110.3923812	jawa
way	569480036	other	\\N	\\N	-7.5325811	110.7304676	jawa
way	570888170	pertamina	Pertamina	Pertamina	-7.0057211	110.4561193	jawa
way	572960772	pertamina	Pertamina	Pertamina	-7.3791965	112.6281644	jawa
way	580798428	pertamina	SPBU Pertamina	SPBU 44.502.34 Cangkiran	-7.0844085	110.310393	jawa
way	591929566	pertamina	Pertamina	SPBU Pertamina 44.552.14 Gedong Tengen	-7.7919803	110.3573057	jawa
way	592229742	pertamina	SPBU Pertamina	SPBU Pertamina	-7.1063623	110.2820768	jawa
way	593690393	pertamina	Pertamina	SPBU Pertamina 44.551.15 Sentul	-7.8020203	110.3784103	jawa
way	593748110	pertamina	Pertamina	SPBU Pertamina 44.513.02 Suking	-6.9666813	110.2782035	jawa
way	598574268	other	\\N	\\N	-6.1585526	106.9015527	sumatra
way	601577463	pertamina	SPBU	SPBU	-7.7471748	108.0178514	jawa
way	604187311	other	\\N	\\N	-6.9866154	108.4916938	jawa
way	605465415	pertamina	Pertamina	Pertamina	-7.3692921	108.025459	jawa
way	609168726	other	Pom Bensin 73	Pom Bensin 73	-7.7654863	108.170887	jawa
way	609363770	pertamina	SPBE Pertamina	SPBE Pertamina	-7.2367666	108.1900397	jawa
way	609369261	other	Pom Bensin Rancamaya	Pom Bensin Rancamaya	-7.3598991	108.1077152	jawa
way	610132421	pertamina	Pertamina	SPBU Pertamina 44.552.16 Munggur	-7.7855579	110.38792	jawa
way	610463974	pertamina	Pertamina	SPBU Pertamina 44.552.15 Tugu	-7.7817046	110.3675272	jawa
way	613259922	pertamina	Pertamina	SPBU Pertamina Pandak	-7.9110498	110.2937964	jawa
way	613683634	pertamina	Pertamina	SPBU 34-16817	-6.40707	106.9741923	jawa
way	615162877	pertamina	Pertamina	SPBU Pertamina 44.552.03 Banguntapan	-7.7983378	110.4083209	jawa
way	616484707	pertamina	PT Pertamina	SPBU	-6.3174679	106.1192314	jawa
way	616902910	pertamina	Pertamina	Pertamina	-6.6742519	107.4392996	jawa
way	621022713	pertamina	Pertamina	SPBU Pertamina 44.552.08 Monjali	-7.7601201	110.3692482	jawa
way	621662925	other	SPBG PGN BOGOR	SPBG PGN BOGOR	-6.590946	106.7879651	jawa
way	625507107	pertamina	SPBU Kalianda	SPBU Kalianda	-5.7274435	105.6475457	sumatra
way	628063780	pertamina	Pertamina	Stasiun Pengisian Bahan Bakar Umum	-7.4621551	112.4398324	jawa
way	629268190	pertamina	Pertamina	SPBU Pertamina 44.513.23 Penaruban	-6.9701327	110.06089	jawa
way	629413181	pertamina	Pertamina	SPBU Pertamina 44.513.19 Wonotenggang	-6.9549808	110.0903591	jawa
way	629721190	other	\\N	\\N	-6.3290089	106.449523	jawa
way	629756263	pertamina	Pertamina	Pertamina	-6.2380655	106.5834763	jawa
way	629934302	other	Pom Bensin Babakan	Pom Bensin Babakan	-7.389099	109.3415787	jawa
way	630185157	pertamina	Pertamina	SPBU Pertamina 44.595.13 Sidorejo Karangawen	-7.0483626	110.5921994	jawa
way	630285773	pertamina	SPBU Cikande	SPBU Cikande	-6.2075705	106.3594097	jawa
way	630579761	pertamina	Pertamina	\\N	-6.6403521	106.7860439	jawa
way	630685563	pertamina	Pertamina	Pertamina 44.561.09 Purwosari	-7.4685303	110.2515945	jawa
way	631045460	pertamina	Pertamina	Pertamina	-7.4138036	112.5435013	jawa
way	631161581	pertamina	Pertamina	Pertamina	-7.3525036	112.6917772	jawa
way	631341111	pertamina	Pertamina	SPBU 34-16919	-6.4441372	106.8715493	jawa
way	631411313	pertamina	Pertamina	Pertamina	-6.3180711	106.5872506	jawa
way	631447471	pertamina	Pertamina	Pertamina	-6.3265401	106.6036103	jawa
way	632638126	pertamina	Pertamina	Pertamina	-6.261439	106.6536028	jawa
way	632643709	shell	Shell	Shell	-6.2580482	106.6522344	jawa
way	632668909	pertamina	Pertamina	Pertamina	-6.2438793	106.64931	jawa
way	633376137	pertamina	Pertamina	Pertamina	-6.3566303	106.0315627	jawa
way	633581790	pertamina	Pertamina	Pertamina	-6.9605927	107.6387438	jawa
way	636169232	pertamina	Pertamina	Pertamina	-6.8935818	107.585959	jawa
way	637399323	pertamina	Pertamina	SPBU Pertamina 54.612.06 Tanggulangin	-7.4994091	112.7100159	jawa
way	637404726	pertamina	Pertamina	SPBU Pertamina 44.574.22 Cawas	-7.7584568	110.6943341	jawa
way	637422956	pertamina	SPBU Bondansari	SPBU Bondansari	-6.8918461	109.6107693	jawa
way	638470940	pertamina	Pertamina	Pertamina	-6.8950995	107.5892089	jawa
way	638905749	pertamina	SPBU Jalan Bhayangkara	SPBU Jalan Bhayangkara	-7.4722479	112.439381	jawa
way	639586206	pertamina	Pertamina	Pertamina	-6.39741	105.9591473	jawa
way	640833670	pertamina	SPBU Bagorejo	SPBU Bagorejo	-8.307368	113.4387439	jawa
way	645525740	pertamina	Pertamina	SPBU 44.592.12	-6.6742226	111.4570416	jawa
way	645873293	pertamina	SPBU Sukowidi	SPBU Sukowidi	-8.1904642	114.3777887	jawa
way	646672092	pertamina	SPBU Tanjung	SPBU Tanjung	-8.1708692	114.3866531	jawa
way	647079816	pertamina	Pertamina	SPBU Pertamina 44.555.32 Krapyak	-7.6952141	110.3460544	jawa
way	649370789	pertamina	Pertamina	SPBU Pertamina 44.552.04 Adisucipto	-7.7831329	110.4361591	jawa
way	651336313	pertamina	SPBU Baledono	SPBU Baledono	-7.6401684	110.3226926	jawa
way	653930607	bp	BP	BP	-7.2730629	112.7476979	jawa
way	654858683	pertamina	OIL!	Pertamina SP PDT Pondok Tengah	-6.0801799	107.0439093	jawa
way	654898517	other	OIL!	meter pertagas (ep-bbwm-pertagas)	-6.0828403	107.0453406	jawa
way	658373083	pertamina	Pertamina	Pertamina	-6.2750154	106.1206756	jawa
way	659696949	pertamina	Pertamina	Pertamina	-6.2328646	106.8225815	jawa
way	662279926	other	\\N	\\N	-7.6385665	110.6908366	jawa
way	662878250	pertamina	Pertamina	SPBU Pertamina 44.513.06 Brangsong	-6.9442047	110.2223029	jawa
way	664459870	pertamina	Pertamina	SPBU 3442118	-6.1539356	106.1904342	sumatra
way	664459871	pertamina	Pertamina	SPBU 3442401	-6.0247348	106.0692487	sumatra
way	664459872	pertamina	Pertamina	SPBU 3442413	-6.0496644	106.062845	sumatra
way	664459874	pertamina	Pertamina	SPBU 3442406	-5.9840346	106.0161596	sumatra
way	664459875	pertamina	Pertamina	SPBU 3442402	-5.9689634	106.0045094	sumatra
way	666594489	pertamina	Pertamina	SPBU Pertamina	-6.2484596	106.7016613	jawa
way	666721001	pertamina	Pertamina	Pertamina	-6.3687337	106.023547	jawa
way	668802935	other	\\N	\\N	-7.1807958	113.4789909	jawa
way	669873111	pertamina	Pertamina	Pertamina	-7.2138011	112.7241485	jawa
way	670830579	shell	Shell	Shell Fatmawati-1 SOUTH JKT	-6.2899285	106.7955854	jawa
way	670831635	pertamina	Pertamina	Pertamina	-6.2721019	106.7971831	jawa
way	673615819	pertamina	Pertamina	Pertamina	-6.4021025	107.4476882	jawa
way	673976656	pertamina	SPBU Pertamina 54.641.38	SPBU Pertamina 54.641.38	-7.8294069	112.0182153	jawa
way	682057894	pertamina	Pertamina	Pertamina (Self-Service)	-6.2943055	106.6806529	jawa
way	684681651	pertamina	Pertamina	Pertamina	-6.2998119	106.6684428	jawa
way	687749519	pertamina	Pertamina	SPBU 34-16419	-6.3850116	106.8827345	jawa
way	699919979	pertamina	Pertamina	SPBU 54.671. Cukur Gondang	-7.7152843	113.0320818	jawa
way	703123300	other	Toko Mba W	Toko Mba W	-7.5527116	110.0388349	jawa
way	704194727	pertamina	Pertamina	Pertamina	-6.6030152	107.4262459	jawa
way	704194745	pertamina	Pertamina	Pertamina	-6.6033991	107.4245538	jawa
way	704247344	pertamina	SPBU Bantarbolang	SPBU Bantarbolang	-7.0422046	109.3752013	jawa
way	704250413	pertamina	SPBU Karangmoncol	SPBU Karangmoncol	-7.0986971	109.348131	jawa
way	704509622	other	\\N	\\N	-6.7783054	108.5674827	jawa
way	704511182	pertamina	Pertamina	Pertamina	-6.7782625	108.5666854	jawa
way	705397551	other	\\N	\\N	-7.0437855	107.5656075	jawa
way	707527077	shell	Shell	Shell Karang Tengah	-6.2189028	106.7068575	jawa
way	710316421	pertamina	Pertamina	Pertamina 54.662.24	-8.0845982	111.83972	jawa
way	710480130	pertamina	SPBU Tigaraksa	SPBU Tigaraksa	-6.2595873	106.4854573	jawa
way	712450068	pertamina	SPBU Tangerang	SPBU Tangerang	-6.2303691	106.5010229	jawa
way	712970105	pertamina	Pertamina	Pertamina 44 541 06	-7.6893661	109.989113	jawa
way	714481096	shell	Shell	Shell	-6.2772982	106.7229052	jawa
way	715214461	pertamina	Pertamina	Pertamina	-7.1200594	113.0745448	jawa
way	717191771	pertamina	SPBU Pertamina Kekiling	SPBU Pertamina Kekiling	-5.7274393	105.6476688	sumatra
way	720106716	other	pom bensin	pom bensin	-7.9318775	114.3877012	jawa
way	720893941	pertamina	Pertamina	Pertamina	-6.2860245	106.9228032	jawa
way	720893950	pertamina	Pertamina	Pertamina 34-17413	-6.2931113	106.9611656	jawa
way	720898009	pertamina	Pertamina	Pertamina	-6.288445	106.9392584	jawa
way	720898011	pertamina	Pertamina	Pertamina	-6.3052006	106.9368699	jawa
way	720909508	other	\\N	\\N	-7.4216642	109.2444554	jawa
way	720975757	pertamina	Pertamina	Pertamina	-7.382969	109.3244983	jawa
way	721054979	pertamina	Pertamina	Pertamina	-6.2865746	106.9513746	jawa
way	721087263	pertamina	Pertamina	Pertamina	-6.3160715	107.1393456	jawa
way	721087265	pertamina	Pertamina	Pertamina	-6.2702546	106.9505951	jawa
way	721087266	shell	Shell	Shell Jatibening-1 Bekasi	-6.2584509	106.949481	jawa
way	721087293	pertamina	Pertamina	Pertamina	-6.249011	106.9596128	jawa
way	721108205	pertamina	Pertamina	Pertamina	-6.4672198	107.4450669	jawa
way	721108206	pertamina	Pertamina	Pertamina	-6.4671878	107.444084	jawa
way	721108209	pertamina	Pertamina	Pertamina	-6.6737987	107.4393527	jawa
way	721108217	pertamina	Pertamina	Pertamina	-6.7761243	108.5640197	jawa
way	721108226	pertamina	Pertamina	Pertamina	-6.5094316	107.71723	jawa
way	721108230	pertamina	Pertamina	Pertamina	-6.5078886	107.7225367	jawa
way	721434250	pertamina	Pertamina	SPBU Pertamina 44.574.06 Krapyak	-7.7143032	110.6008961	jawa
way	721493912	pertamina	Pertamina	Pertamina	-6.8991444	107.6271612	jawa
way	721497810	pertamina	Pertamina	Pertamina	-6.9429392	107.6423171	jawa
way	721497811	pertamina	Pertamina	Pertamina	-6.9409118	107.6419023	jawa
way	721506627	pertamina	Pertamina	Pertamina	-6.3297398	106.9460029	jawa
way	721802932	pertamina	Pertamina	Pertamina	-7.8915843	110.3272954	jawa
way	722181533	pertamina	Pertamina	Pertamina	-6.3438741	106.6869655	jawa
way	722189016	pertamina	Pertamina	SPBU Pertamina Pasar Sapi	-7.3379078	110.4990922	jawa
way	722189018	pertamina	Pertamina	Pertamina	-7.3320457	110.4993199	jawa
way	722243140	pertamina	Pertamina	Pertamina	-7.4547078	109.284972	jawa
way	722246803	pertamina	Pertamina	Pertamina	-6.8137373	107.6221208	jawa
way	722397851	other	\\N	\\N	-7.0348156	112.7654519	jawa
way	723904035	pertamina	Pertamina	SPBU Pertamina 54.661.06 Gedog	-8.0898652	112.1855584	jawa
way	723904036	pertamina	Pertamina	SPBU 54.661.04 Kebonrojo	-8.0947332	112.1754931	jawa
way	724188533	pertamina	Pertamina	Pertamina	-7.3003527	112.7695412	jawa
way	724333609	pertamina	Pertamina	Pertamina 44 541 09	-7.707412	110.0149915	jawa
way	724333610	pertamina	Pertamina	Pertamina 44 541 03	-7.727976	109.975117	jawa
way	724333611	pertamina	Pertamina	Pertamina 44 542 01	-7.7224281	109.9427452	jawa
way	724333614	pertamina	Pertamina	Pertamina 44 541 05	-7.7246608	110.024555	jawa
way	726068588	other	POM BENSIN KEDUNGRINGIN	POM BENSIN KEDUNGRINGIN	-8.4510673	114.3151165	jawa
way	728051274	pertamina	Pertamina	Pertamina	-7.2563938	112.7190733	jawa
way	728052468	pertamina	Pertamina	Pertamina	-7.7087324	113.986999	jawa
way	733332909	pertamina	Pertamina	Pertamina	-7.1154432	108.9911309	jawa
way	733339309	pertamina	Pertamina	Pertamina	-7.3184504	109.3612232	jawa
way	737501560	pertamina	SPBU	SPBU	-7.5676453	108.8958408	jawa
way	737501561	pertamina	SPBU	SPBU	-7.5676235	108.8955586	jawa
way	742779333	pertamina	Pertamina	Pertamina	-7.3489511	108.4862416	jawa
way	748874127	pertamina	Pertamina	SPBU 34-13403	-6.2384702	106.8992579	jawa
way	748885521	pertamina	Pertamina	Pertamina	-6.2427092	106.9020449	jawa
way	749894930	shell	Shell	Shell Noer Ali-1 Bekasi	-6.2493887	106.9490449	jawa
way	750001527	pertamina	Pertamina	SPBU 34.10502	-6.1729833	106.8618642	sumatra
way	751442568	bp	BP	BP	-7.2805889	112.7841277	jawa
way	751690008	pertamina	PT Pertamina	SPBU Pertamina 34-131505	-6.2808229	106.8719487	jawa
way	752024790	other	Pommini	Pommini	-6.2720582	106.8770004	jawa
way	753053316	pertamina	PT Pertamina	SPBU 34-14310	-6.1359093	106.8909642	sumatra
way	753133273	pertamina	Pertamina	Pertamina 34-17115	-6.2744279	106.9352392	jawa
way	753358863	pertamina	Pertamina	Pertamina	-6.2704225	106.911666	jawa
way	753427022	pertamina	Pertamina	Pertamina	-6.2139951	106.9052941	jawa
way	753434328	shell	Shell	Shell Cipinang-1 EAST JKT	-6.2152187	106.8796572	jawa
way	753435840	pertamina	PT Pertamina	SPBU 34-14301	-6.1386293	106.86735	sumatra
way	754042450	pertamina	Pertamina	SPBU Pertamina 34-13506	-6.2853854	106.8794935	jawa
way	754061490	pertamina	Pertamina	Pertamina	-6.3068819	106.8755286	jawa
way	754061614	pertamina	Pertamina	SPBU 34-13202	-6.1796577	106.9033767	sumatra
way	754312210	pertamina	Pertamina	Pertamina	-6.2064861	106.9231255	jawa
way	754322335	shell	Shell	Shell Sunter UT-1 NORTH JAKARTA	-6.1384035	106.878996	sumatra
way	754954061	pertamina	Pertamina	SPBU 34-14307	-6.1466499	106.8857216	sumatra
way	754969050	pertamina	Pertamina	SPBU 34-14306	-6.1464211	106.8737344	sumatra
way	755211266	pertamina	SPBU Pertamina	SPBU Pertamina	-6.1600816	106.9744801	sumatra
way	755265544	pertamina	Pertamina	Pertamina	-6.2364388	106.7427072	jawa
way	755286942	pertamina	Pertamina	SPBU 34-14305	-6.1621054	106.8753396	sumatra
way	755292799	shell	Shell	Shell Soepomo-1 SOUTH JKT	-6.2311585	106.8469707	jawa
way	755305132	pertamina	Pertamina	SPBU 34.12901	-6.2130094	106.844366	jawa
way	755313470	pertamina	Pertamina	SPBU Kwitang	-6.1850116	106.8436021	sumatra
way	755341462	pertamina	Shell	SPBU SHELL	-6.1705766	106.8648312	sumatra
way	756035597	pertamina	Pertamina	SPBU 34.10605	-6.1740601	106.8536322	sumatra
way	756066781	pertamina	Pertamina	Pertamina	-6.5504125	106.8215053	jawa
way	756070931	pertamina	Pertamina	\\N	-6.5563315	106.8162665	jawa
way	756071517	pertamina	Pertamina	Pertamina	-6.2613275	106.7390724	jawa
way	756080768	bp	BP	BP	-6.2650765	106.6300231	jawa
way	756086569	pertamina	Pertamina	Pertamina	-6.1748084	106.6527456	sumatra
way	756092861	pertamina	Pertamina	SPBU 34-16109	-6.5661198	106.8102341	jawa
way	756109560	pertamina	Pertamina	\\N	-6.4854198	106.8359644	jawa
way	756299606	pertamina	Pertamina	\\N	-6.3396057	106.8898485	jawa
way	756306343	shell	Shell	Shell	-6.1924531	106.6402706	sumatra
way	756306344	pertamina	Pertamina	Pertamina	-6.1921816	106.6406222	sumatra
way	756326122	pertamina	Pertamina	Pertamina	-6.1928514	106.6375689	sumatra
way	756337726	pertamina	Pertamina	SPBU CIKARET	-6.467843	106.8404892	jawa
way	756364375	shell	Shell	Shell	-6.1976859	106.6289969	sumatra
way	756374063	pertamina	Pertamina	Pertamina	-6.2617116	106.7567392	jawa
way	756400909	pertamina	Pertamina	Pertamina	-6.2313584	106.6812873	jawa
way	756614984	shell	Shell	Shell	-6.2252522	106.6077949	jawa
way	756673725	pertamina	Pertamina	Pertamina	-6.2229422	106.6930675	jawa
way	756679458	pertamina	Pertamina	Pertamina	-6.2369861	106.7529935	jawa
way	756759033	pertamina	Pertamina	Pertamina	-6.1969501	106.583588	sumatra
way	757042422	pertamina	Pertamina	Pertamina	-6.1936131	106.7978963	sumatra
way	757079027	pertamina	Pertamina	Pertamina	-6.2016005	106.573709	jawa
way	757092682	pertamina	Pertamina	SPBU Sukabakti, Ciputat	-6.3062054	106.7189477	jawa
way	757324850	pertamina	Pertamina	Pertamina	-6.3011209	106.7146957	jawa
way	757363379	pertamina	Pertamina	Pertamina	-6.2799823	106.7175913	jawa
way	757371455	pertamina	Pertamina	Pertamina	-6.3005105	106.7313061	jawa
way	758191187	pertamina	VIVO	SPBU VIVO Bintaro Sektor 7	-6.2751096	106.7228911	jawa
way	758420607	pertamina	Pertamina	Pertamina	-6.2859626	106.7259883	jawa
way	758431920	bp	BP	BP	-6.2739445	106.7013823	jawa
way	759063916	pertamina	Pertamina	SPBU 34-13604 Pertamina	-6.2571362	106.8638155	jawa
way	759648346	pertamina	Pertamina	SPBU 3410301	-6.1914458	106.8282531	sumatra
way	759657164	shell	Shell	Shell V-Power	-6.112675	106.7530694	sumatra
way	759692258	pertamina	Pertamina	Pertamina	-6.1062811	106.7411112	sumatra
way	759778524	pertamina	Pertamina	Pertamina 33-142-01	-6.1485168	106.9167535	sumatra
way	759886379	shell	Shell	Shell	-6.2619301	106.6532039	jawa
way	759888431	pertamina	Pertamina	Pertamina	-6.2425808	106.8524768	jawa
way	759916848	pertamina	PT Pertamina	SPBU 34.102.06	-6.2290297	106.7972921	jawa
way	760194784	pertamina	Pertamina	Pertamina 34-14208	-6.145175	106.8950971	sumatra
way	760197197	pertamina	Pertamina	Pertamina 34-14210	-6.1469971	106.8967045	sumatra
way	760258359	pertamina	Pertamina	Pertamina	-6.2428058	106.7877224	jawa
way	760393990	pertamina	Pertamina	Pertamina	-6.23961	106.8263581	jawa
way	760400332	pertamina	PT Pertamina	SPBU 34 10201	-6.2025144	106.8062714	jawa
way	760401836	pertamina	Pertamina	Pertamina	-6.3005974	106.8405896	jawa
way	760411099	pertamina	Pertamina	Pertamina	-6.2717751	106.8380912	jawa
way	760412744	pertamina	Pertamina	Pertamina	-6.1666343	106.9152124	sumatra
way	760412998	pertamina	PT Pertamina	SPBU 34-12805	-6.2252262	106.8411737	jawa
way	760421731	pertamina	Pertamina	34-14101	-6.1437872	106.9224316	sumatra
way	760422860	pertamina	Pertamina	Pertamina	-6.1801164	106.1471216	sumatra
way	760595162	pertamina	Pertamina	Pertamina 34-14206	-6.152996	106.9163845	sumatra
way	760606613	pertamina	Pertamina	SPBU Envogas 31.A.139.04	-6.190972	106.9064329	sumatra
way	760612441	pertamina	Pertamina	Pertamina	-6.1182753	106.9247363	sumatra
way	760615924	pertamina	Pertamina	Pertamina	-6.2285785	106.8338428	jawa
way	760621458	pertamina	Pertamina	Pertamina	-6.2870969	106.8293188	jawa
way	760622611	pertamina	Pertamina	Pertamina	-6.2915186	106.8118446	jawa
way	760622793	shell	Shell	Shell TB Simatupang-1 SOUTH JKT	-6.2929395	106.8000109	jawa
way	760626696	pertamina	Pertamina	Pertamina	-6.1303009	106.8056942	sumatra
way	761283960	pertamina	PT Pertamina	SPBU 34-11301	-6.1486797	106.7947571	sumatra
way	761286148	pertamina	Pertamina	Pertamina 31.103.03	-6.1960658	106.8404951	sumatra
way	761325352	pertamina	Pertamina	Pertamina	-6.1408273	106.8271756	sumatra
way	761496850	pertamina	Pertamina	Pertamina	-6.1567085	106.8188965	sumatra
way	761563491	pertamina	Pertamina	Pertamina	-6.3025236	106.8530387	jawa
way	761569122	pertamina	Pertamina	Pertamina	-6.1558607	106.7923531	sumatra
way	761581084	pertamina	Pertamina	Pertamina	-6.2402684	106.8204481	jawa
way	761618374	pertamina	Pertamina	SPBG Jakprogas	-6.1709165	106.886565	sumatra
way	761769380	pertamina	Pertamina	Pertamina	-6.1247539	106.7963283	sumatra
way	761786313	shell	Shell	Shell	-6.1257316	106.7984309	sumatra
way	761787462	pertamina	Pertamina	Pertamina	-6.2213945	106.7905415	jawa
way	761796432	shell	Shell	Shell A. Gading-1 NORTH JKT	-6.1658948	106.9150137	sumatra
way	761797534	pertamina	Pertamina	Pertamina	-6.2286685	106.7782193	jawa
way	761841671	other	Peteramina	Peteramina	-6.1785597	106.8181865	sumatra
way	762013752	pertamina	Pertamina	Pertamina	-6.1981704	106.8031036	sumatra
way	762025115	pertamina	Pertamina	Pertamina	-6.2734097	106.5980272	jawa
way	762030079	pertamina	Pertamina	Pertamina	-6.261227	106.6056335	jawa
way	762092674	pertamina	Pertamina	Pertamina	-6.1991696	106.6967389	sumatra
way	762105880	pertamina	Pertamina	SPBU Pertamina 31.114.04	-6.1763909	106.8016765	sumatra
way	762118466	shell	Shell	Shell	-6.3418087	106.7373335	jawa
way	762285018	pertamina	Pertamina	Pertamina	-6.3148218	106.7013869	jawa
way	762304133	pertamina	Pertamina	Pertamina	-6.323252	106.6837259	jawa
way	762304338	pertamina	Pertamina	Pertamina	-6.3164153	106.6781676	jawa
way	762304840	bp	BP	BP	-6.3204762	106.681732	jawa
way	762327678	pertamina	Pertamina	Pertamina	-6.2909792	106.731267	jawa
way	762334390	pertamina	Pertamina	Pertamina	-6.3269597	106.747572	jawa
way	762372667	pertamina	Pertamina	Pertamina	-6.1789643	106.6592673	sumatra
way	762896006	pertamina	Pertamina	SPBU	-6.3459577	106.7126784	jawa
way	762905323	pertamina	Pertamina	Pertamina	-6.3446051	106.7201525	jawa
way	762966646	pertamina	Pertamina	Pertamina	-6.3485835	106.7371373	jawa
way	762969054	pertamina	Pertamina	Pertamina	-6.3135097	106.6923242	jawa
way	762984285	pertamina	Pertamina	Pertamina 34-17117	-6.2089562	106.9774361	jawa
way	762988167	pertamina	Pertamina	Pertamina 31.171.05	-6.2016857	106.9764999	jawa
way	763178874	pertamina	Pertamina	32-14201	-6.156662	106.8864486	sumatra
way	763248609	pertamina	Pertamina	SPBU	-6.3124552	106.7518677	jawa
way	763255908	pertamina	Pertamina	Pertamina	-6.1913578	106.6689442	sumatra
way	763415980	pertamina	Pertamina	Pertamina	-6.1752566	106.8482956	sumatra
way	763444268	pertamina	Pertamina	Pertamina	-6.1735953	106.6791401	sumatra
way	763451717	pertamina	VIVO	SPBU VIVO Bekasi	-6.2313366	106.9872385	jawa
way	764237191	shell	Shell	Shell	-6.1853441	106.6492254	sumatra
way	764237196	pertamina	Pertamina	Pertamina	-6.1848772	106.6500765	sumatra
way	764282277	pertamina	Pertamina	Pertamina	-6.233756	106.9943725	jawa
way	764297637	pertamina	PT Pertamina	SPBU 34-13901	-6.2117548	106.8901968	jawa
way	765309802	pertamina	Pertamina	SPBU Pertamina	-6.2885997	107.3804259	jawa
way	770592896	pertamina	Pertamina	\\N	-6.3037698	107.3237193	jawa
way	777092837	pertamina	Pertamina	Pertamina	-6.9658485	107.5757133	jawa
way	779238895	pertamina	Pertamina	Pertamina	-6.9702438	107.5746338	jawa
way	781168398	pertamina	Pertamina	\\N	-6.2899516	107.2931054	jawa
way	790768303	pertamina	PT Pertamina	SPBU Pertamina 4458111	-7.0584423	110.66797	jawa
way	793866237	pertamina	Pertamina	Pertamina	-7.3552983	108.5803882	jawa
way	793898629	pertamina	Pertamina	Pertamina	-7.3660005	108.5438146	jawa
way	794848563	pertamina	Pertamina	Pertamina	-7.3707391	108.5417424	jawa
way	795102225	pertamina	Pertamina	Pertamina	-7.363967	108.5348823	jawa
way	800643643	pertamina	Pertamina	SPBU Pertamina 54.663.03 KARANGAN	-8.0802564	111.6645321	jawa
way	802794854	other	\\N	\\N	-6.8348386	108.2325123	jawa
way	814574662	pertamina	Pertamina	SPBU Pertamina 44.521.18 Bongkok	-6.8809726	109.1872866	jawa
way	814745762	pertamina	Pertamina	Pertamina	-7.0853707	107.7071369	jawa
way	818650148	pertamina	Pertamina	SPBU Pertamina 34-46212	-7.3304275	108.3635808	jawa
way	819819642	pertamina	Pertamina	SPBU Pertamina	-7.7902929	111.5173684	jawa
way	819819643	pertamina	Pertamina	Pertamina	-7.7683748	111.5271197	jawa
way	819819644	other	\\N	\\N	-7.8194408	111.4991443	jawa
way	824631139	pertamina	Pertamina	SPBU Pertamina 34.40322 Manirancan	-7.0316269	107.7515354	jawa
way	825869228	pertamina	Pertamina	SPBU Pertamina	-7.6824614	111.5351036	jawa
way	825869229	pertamina	Pertamina	SPBU Pertamina	-7.6550156	111.524052	jawa
way	826376437	pertamina	Pertamina	Pertamina	-6.6512258	106.8853907	jawa
way	841988885	shell	Shell	Shell	-6.2210844	106.6563097	jawa
way	845024555	other	\\N	\\N	-6.5380203	107.6927653	jawa
way	851025690	pertamina	Pertamina	Pertamina	-7.4444968	109.2734193	jawa
way	856836119	other	\\N	\\N	-6.1654496	106.7636341	sumatra
way	857364239	pertamina	Pertamina	SPBU 4552127 KARANGJATI TARUB	-6.9440498	109.1843173	jawa
way	858635520	other	\\N	\\N	-6.805972	107.5958509	jawa
way	863010641	pertamina	Pertamina	Pertamina	-6.8399285	107.4781669	jawa
way	865664766	pertamina	Pertamina	Pertamina	-7.373802	108.5197128	jawa
way	866190923	pertamina	Pertamina	Pertamina	-7.8869393	110.0693335	jawa
way	875909753	pertamina	Pertamina	Pertashop	-7.3200129	109.3383012	jawa
way	877546071	bp	BP	BP	-6.3143113	106.6967487	jawa
way	877546073	shell	Shell	Shell	-6.3106309	106.6886842	jawa
way	879562441	pertamina	Pertamina	SPBU Pertamina 44.521.19 Tarub	-6.9039247	109.1879339	jawa
way	882361255	pertamina	Pertamina	Pertamina	-7.4100142	111.3803716	jawa
way	882361264	pertamina	Pertamina	Pertamina	-7.4086576	111.3817274	jawa
way	886688589	pertamina	Pertamina	Pertamina	-8.3662337	114.1651972	jawa
way	886809426	pertamina	Pertamina	Pertamina	-8.4295281	114.1023639	jawa
way	886813088	other	\\N	\\N	-8.3559786	114.1279773	jawa
way	886816816	pertamina	Pertamina	Pertamina	-8.4075783	114.164923	jawa
way	892730367	pertamina	Pertamina	SPBU Pertamina 44.556.06 Sogan	-7.890105	110.1136628	jawa
way	892730820	pertamina	Pertamina	SPBU Pertamina 44.556.10 Temon	-7.8896258	110.0993669	jawa
way	893915243	pertamina	SPBU Weru	SPBU Weru	-7.4461148	111.5631421	jawa
way	894257756	pertamina	Pertamina	SPBU Pertamina 44.521.10 Tangkil Mindaka	-6.9247729	109.1872392	jawa
way	894584751	other	\\N	\\N	-6.740329	111.0248088	jawa
way	896452487	pertamina	Pertamina	SPBU Pertamina 44.574.20 Cokro	-7.6016081	110.6460268	jawa
way	898215943	other	\\N	\\N	-6.9547896	108.0949117	jawa
way	902573085	pertamina	PT Pertamina	SPBU 34-17410	-6.3521805	106.9215772	jawa
way	906634148	pertamina	Pertamina	Pertamina	-6.3264009	107.2924895	jawa
way	906960710	pertamina	Pertamina	Pertamina	-6.2772364	107.0196271	jawa
way	906964413	pertamina	Pertamina	\\N	-7.7612381	109.6684189	jawa
way	906966825	pertamina	Pertamina	Pertamina	-6.3294232	107.311797	jawa
way	909627605	pertamina	PERTAMINA	SPBU 34-15712	-6.2233463	106.5438433	jawa
way	913778189	pertamina	Pertamina	Pertamina	-6.3758992	106.682325	jawa
way	926493927	pertamina	SPBU	SPBU	-6.2923351	107.0035599	jawa
way	934759659	pertamina	Pertamina	\\N	-7.5439728	112.387099	jawa
way	948902925	pertamina	Pertamina	SPBU Pertamina 44.543.17	-7.7207271	109.8227448	jawa
way	968159502	pertamina	Pertamina	Pertamina	-6.8901183	107.6128827	jawa
way	968677743	other	\\N	\\N	-6.8958078	113.4747837	jawa
way	972284017	pertamina	SPBU Pertamina 44.512.04 Subah	SPBU Pertamina 44.512.04 Subah	-6.9772848	109.8905714	jawa
way	975861339	pertamina	Pertamina	Pertamina	-6.3472381	106.6910458	jawa
way	975861344	pertamina	Pertamina	Pertamina	-6.3466582	106.6970666	jawa
way	979447645	pertamina	Pertamina	Pertamina	-6.3221426	106.7088363	jawa
way	980808699	other	Pompa Bensin Mini Pertamax	Pompa Bensin Mini Pertamax	-6.6599814	111.0241185	jawa
way	980808700	pertamina	Pertamina	Pompa bensin Tlogowungu	-6.7075352	111.0140518	jawa
way	988487767	other	Pom Bensin	Pom Bensin	-6.5433049	107.7259077	jawa
way	989304954	other	\\N	\\N	-6.5433253	107.7260052	jawa
way	989774209	shell	Shell	Shell	-6.3676223	106.9693488	jawa
way	991619248	pertamina	Pertamina	Pertamina	-6.1028565	106.162739	sumatra
way	995052709	pertamina	SPBU Pertamina	SPBU Pulosari	-7.5243387	110.7490466	jawa
way	996737919	pertamina	Pertamina	SPBU Pertamina 44.572.19 Kalijambe	-7.451645	110.8057733	jawa
way	999080596	pertamina	Pertamina	Pertamina	-6.9711868	109.6054127	jawa
way	999479811	pertamina	Pertamina	SPBU 54.623.05	-6.9062982	112.0725939	jawa
way	1001045622	pertamina	SPBU Pertamina 34-42314	SPBU Pertamina 34-42314	-6.5438335	106.389513	jawa
way	1002800906	pertamina	Pertamina	SPBU 44.577.07	-7.6116247	110.9478805	jawa
way	1002804970	pertamina	Pertamina	SPBU Pertamina 44.577.05 Gondangrejo	-7.4898947	110.8113335	jawa
way	1004150019	other	\\N	\\N	-7.5760874	110.9338393	jawa
way	1004611384	pertamina	SPBU Pertamina	SPBU Papahan	-7.5873405	110.9201198	jawa
way	1006823526	other	Pombensin Karang satria	Pombensin Karang satria	-6.2134207	107.0337672	jawa
way	1013586431	pertamina	Pertamina	SPBU 54.623.18	-6.8781597	112.0423995	jawa
way	1014392383	pertamina	Pertashop	Pertashop	-7.2994721	109.3355079	jawa
way	1014489389	pertamina	Pertamina	SPBU 54.623.12	-6.8512866	112.0201465	jawa
way	1016773732	pertamina	Pertamina	SPBU Pertamina 44.551.19 Timoho	-7.7981112	110.3928253	jawa
way	1017759321	pertamina	Pertamina	SPBU Pertamina 54.661.07	-8.112524	112.1560204	jawa
way	1020078718	pertamina	Pertamina	Pertamina	-5.7260813	105.6692065	sumatra
way	1020165746	pertamina	Pertamina	SPBU Pertamina 44.557.01 Imogiri Timur	-7.8650308	110.3893358	jawa
way	1020213648	pertamina	Pertamina	SPBU 55.661.44 Plosorejo	-8.1524736	112.1605	jawa
way	1020471567	pertamina	Pertamina	SPBU Pertamina 44.558.09 Gading	-7.9095019	110.5534531	jawa
way	1023991364	pertamina	Pertamina	SPBU 54.623.17	-6.9032915	112.08035	jawa
way	1026675567	pertamina	Pertamina	Pertamina	-6.9234503	107.6278685	jawa
way	1026675568	pertamina	Pertamina	Pertamina	-6.9231756	107.6278679	jawa
way	1027286918	pertamina	Pertamina	SPBU 54.633.09	-7.6744052	111.4888146	jawa
way	1034678317	pertamina	SPBU KEDOKAN AGUNG 3445227	SPBU KEDOKAN AGUNG 3445227	-6.5072926	108.4262878	jawa
way	1039233378	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5783641	106.8181541	jawa
way	1042000718	pertamina	Pertamina	SPBU 53.623.29	-6.9357712	112.0556301	jawa
way	1046717303	pertamina	Pertamina	Pertamina	-6.6514835	106.8898268	jawa
way	1052579829	pertamina	Pertamina	SPBU Kanigoro Kalipuro Pungging Mojopahit	-7.5381878	112.5740341	jawa
way	1053215622	pertamina	Pertamina	Pertamina	-6.1537704	106.5849329	sumatra
way	1059441463	pertamina	SPBU Pertamina	SPBU Pertamina	-6.7560912	111.0211878	jawa
way	1061258451	pertamina	Pertamina	SPBU Pertamina 54.662.25	-8.0736567	111.9101518	jawa
way	1062634670	pertamina	Pertamina	Pertamina	-6.3997799	106.826034	jawa
way	1062634671	shell	Shell	Shell Siliwangi	-6.3991918	106.8222865	jawa
way	1063096592	pertamina	Pertamina	SPBU Pertamina 54.662.29	-8.1382129	111.996645	jawa
way	1063448778	pertamina	Pertamina	SPBG Pertamina Depok	-6.40341	106.8344619	jawa
way	1063448779	shell	Shell	Shell	-6.3943677	106.7869947	jawa
way	1063448780	shell	Shell	Shell Meruyung	-6.3854095	106.7684531	jawa
way	1064208623	pertamina	Pertamina	SPBU Pertamina 44.574.02 Karangwuni	-7.6748438	110.6538153	jawa
way	1069013935	pertamina	Pertamina	SPBU Pertamina 44.551.11 Giwangan	-7.8362685	110.3930688	jawa
way	1071789922	pertamina	Pertamina	Pertamina 34.17518	-6.2917771	107.0312063	jawa
way	1072138407	pertamina	Pertamina	Pertamina SPBU 34-17538	-6.2794459	107.0402724	jawa
way	1072138409	shell	Shell	Shell	-6.2767703	107.0451247	jawa
way	1072938510	pertamina	Pertamax Pom bensin/SPBU	Pertamax Pom bensin/SPBU	-7.9694113	110.853888	jawa
way	1073264831	pertamina	Pertamina	Pertamina	-7.5760125	110.6222921	jawa
way	1073821005	pertamina	Pertamina	SPBU	-7.214052	107.9045894	jawa
way	1073826332	shell	Shell	Shell	-6.3033404	106.7235576	jawa
way	1074390555	pertamina	Pertamina	Pertamina	-7.1037552	107.8979175	jawa
way	1074882383	pertamina	Pertamina	SPBU 34-43226, Cimacan , Puncak	-6.7151792	107.0243081	jawa
way	1075954586	pertamina	SPBU 34.45165 Tegalsari	SPBU 34.45165 Tegalsari	-6.7014953	108.4934818	jawa
way	1076762237	pertamina	SPBU Pertamina 44.572.02 Sine	SPBU Pertamina 44.572.02 Sine	-7.4311057	111.0046308	jawa
way	1077368582	pertamina	Pertamina	Pertamina	-6.2584714	106.926679	jawa
way	1077368583	pertamina	Pertamina	Pertamina	-6.2584418	106.9283869	jawa
way	1079765899	pertamina	Pertamina	SPBU Pertamina 44.513.08 Weleri	-6.9659554	110.0791359	jawa
way	1079924413	pertamina	Pertamina	Pertamina	-6.7324666	111.0472252	jawa
way	1081041702	pertamina	PT Pertamina	SPBU 34 11612	-6.1978803	106.7419563	sumatra
way	1081042356	pertamina	SPBU Nganguk	SPBU Nganguk	-6.8057847	110.847364	jawa
way	1081413298	shell	Shell	Shell	-6.3300699	107.2968404	jawa
way	1085492294	pertamina	Pertamina	Pertamina	-6.7004142	111.3138838	jawa
way	1087412649	pertamina	SPBU 44.5767 Cengkal	SPBU 44.5767 Cengkal	-7.903489	110.8530098	jawa
way	1087412650	pertamina	SPBU 44.5767 Cengkal	SPBU 44.5767 Cengkal	-7.9034297	110.8528353	jawa
way	1091862906	pertamina	VIVO	SPBU VIVO Parung	-6.4482816	106.7322218	jawa
way	1092529417	pertamina	Pertamina	SPBU Jalan Cagak	-6.6756921	107.6929544	jawa
way	1093032202	other	PBU Nglangon	PBU Nglangon	-7.4131116	111.0167285	jawa
way	1093068229	pertamina	Pertamina	Pertamina 34.463.09	-7.4105495	108.5469691	jawa
way	1093752171	pertamina	Pertamina	SPBU Pertamina Taman Asri	-7.4348423	111.0242038	jawa
way	1095166017	pertamina	Pertamina	SPBU Pertamina 54.662.04	-8.0686198	111.9070935	jawa
way	1096053018	pertamina	Pertamina	SPBU Pertamina 44.556.11 Pengasih	-7.8327623	110.181078	jawa
way	1096061063	pertamina	Pertamina	SPBE Pertamina	-7.860371	110.2324529	jawa
way	1096316055	other	\\N	\\N	-7.4018898	109.3483169	jawa
way	1096393982	bp	BP	\\N	-6.2313144	106.7897719	jawa
way	1097116866	pertamina	Pertamina	\\N	-6.2422667	106.7125995	jawa
way	1097500518	shell	Shell	Shell Veteran Gresik	-7.1873314	112.6470488	jawa
way	1097928333	pertamina	SPBU Gembong	SPBU Gembong	-7.3690863	109.3559947	jawa
way	1097929420	pertamina	SPBU Bojongsari	SPBU Bojongsari	-7.358382	109.3558579	jawa
way	1101932568	pertamina	Pertamina	SPBU Pertamina 54.662.27	-8.175101	111.9686732	jawa
way	1102403072	other	\\N	\\N	-8.2264672	111.9577929	jawa
way	1103038152	pertamina	SPBU Pertamina 54.621.05 Kebonagung	SPBU Pertamina 54.621.05 Kebonagung	-7.1622449	111.6367588	jawa
way	1103038153	pertamina	SPBU Pertamina 54.621.19 Syirkah Amanah 2 Kalitidu	SPBU Pertamina 54.621.19 Syirkah Amanah 2 Kalitidu	-7.1484942	111.7014095	jawa
way	1103038154	pertamina	SPBU Pertamina 54.621.07 Mlaten Kalitidu	SPBU Pertamina 54.621.07 Mlaten Kalitidu	-7.1309894	111.7407482	jawa
way	1103040628	pertamina	SPBU Pertamina 53.621.20 Purworejo Padangan	SPBU Pertamina 53.621.20 Purworejo Padangan	-7.185916	111.5976196	jawa
way	1103040632	pertamina	SPBU Pertamina 54.621.22 Kerdu	SPBU Pertamina 54.621.22 Kerdu	-7.1976359	111.5897472	jawa
way	1103040638	pertamina	SPBU Pertamina 54.621.08 Ngraho	SPBU Pertamina 54.621.08 Ngraho	-7.2234506	111.5607323	jawa
way	1103040642	pertamina	SPBU Pertamina 54.621.18 Panjunan Kalitidu	SPBU Pertamina 54.621.18 Panjunan Kalitidu	-7.1301074	111.7789198	jawa
way	1103042379	pertamina	SPBU Pertamina 54.621.04 Kalianyar Kapas	SPBU Pertamina 54.621.04 Kalianyar Kapas	-7.1733635	111.8988872	jawa
way	1103042381	pertamina	SPBU Pertamina 54.621.11 Syirkah Amanah Balen	SPBU Pertamina 54.621.11 Syirkah Amanah Balen	-7.1935445	111.9563313	jawa
way	1103042385	pertamina	SPBU Pertamina 53.621.25 Kedungbondo Balen	SPBU Pertamina 53.621.25 Kedungbondo Balen	-7.1841981	111.9846975	jawa
way	1103042387	pertamina	SPBU Pertamina 54.621.02 Prayungan	SPBU Pertamina 54.621.02 Prayungan	-7.1663199	112.0363674	jawa
way	1103044047	pertamina	SPBU Pertamina 54.621.24 Sraturejo Baureno	SPBU Pertamina 54.621.24 Sraturejo Baureno	-7.1393571	112.0831495	jawa
way	1103044050	pertamina	SPBU Pertamina 54.621.14 Trojalu Baureno	SPBU Pertamina 54.621.14 Trojalu Baureno	-7.1291087	112.1211911	jawa
way	1103046423	pertamina	SPBU Pertamina 54.621.09 Dander	SPBU Pertamina 54.621.09 Dander	-7.232312	111.8582377	jawa
way	1103049890	pertamina	Pertamina	Pertashop Winong	-8.0438801	111.9007572	jawa
way	1103244392	pertamina	Pertamina	SPBU 34-43315	-6.8953949	106.7817697	jawa
way	1103359853	pertamina	SPBU Pertamina 54.621.06 Ngrowo	SPBU Pertamina 54.621.06 Ngrowo	-7.147226	111.8937029	jawa
way	1103385728	pertamina	SPBU Pertamina 54.621.13 Veteran	SPBU Pertamina 54.621.13 Veteran	-7.1578678	111.8989079	jawa
way	1105143243	other	TNI	\\N	-6.3420723	106.6925189	jawa
way	1109624776	pertamina	Pertamina	SPBU 44.561.15 Cacaban	-7.4766957	110.2139955	jawa
way	1109900388	pertamina	Pertamina	SPBU Pertamina	-6.2409543	106.7560218	jawa
way	1109903313	shell	Shell	Shell	-6.2304579	106.6888379	jawa
way	1110288000	pertamina	SPBU Jendral Sudirman	SPBU Jendral Sudirman	-6.8047529	110.8506457	jawa
way	1110653951	pertamina	Pertamina	SPBU Pertamina 44.576.05 Nanggan Selogiri	-7.7754599	110.8896017	jawa
way	1110656490	pertamina	SPBU Pokoh	SPBU Pokoh	-7.81322	110.9353397	jawa
way	1112996971	shell	Shell	Shell	-6.3170741	106.6800252	jawa
way	1114999717	other	Area Pom Bensin	Area Pom Bensin	-7.6542743	112.1576662	jawa
way	1121941444	bp	BP	\\N	-6.3209216	106.8361482	jawa
way	1123915746	shell	Shell	Shell	-6.9014423	107.6505327	jawa
way	1123999692	pertamina	Pertamina	Pertamina	-6.9260197	107.7104689	jawa
way	1125427519	pertamina	SPBU Pertamina 44.531.33	SPBU Pertamina 44.531.33	-7.3978361	109.2315409	jawa
way	1126131321	pertamina	Pertamina	Pertamina	-7.2604259	112.7543075	jawa
way	1128222296	pertamina	Pertamina	SPBU 44.575.21 DALANGAN SKH	-7.7219912	110.8065333	jawa
way	1130595961	pertamina	Pertamina	Pertashop	-7.7030045	110.7367592	jawa
way	1131234864	pertamina	Pertamina	SPBU 64.572.21	-7.7903095	113.2826712	jawa
way	1132361553	pertamina	Pertamina	SPBU Pertamina 34-12208	-6.2488039	106.7778497	jawa
way	1132679107	other	Mobil	MOBIL Indostation Mampir	-6.4249779	107.0060569	jawa
way	1133030695	pertamina	Pertamina	Pertamina 34-1714000	-6.203581	107.0031648	jawa
way	1133030696	pertamina	Pertamina	Pertamina 34-17136	-6.2068609	107.0174771	jawa
way	1133030697	pertamina	Pertamina	Pertamina 34-17137	-6.2269268	106.98681	jawa
way	1133030698	pertamina	Pertamina	Pertamina 34-17107	-6.2219183	107.0155057	jawa
way	1134395863	pertamina	Pertamina	Pertashop	-7.7446765	110.4716936	jawa
way	1134678114	pertamina	Pertamina	SPBU 54.633.03	-7.6825289	111.4204233	jawa
way	1134705281	pertamina	Pertamina	SPBU Pertamina 44.571.32 Transito	-7.5627639	110.7921324	jawa
way	1136688868	pertamina	Pertamina	SPBU 44.592.18	-6.689696	111.4277589	jawa
way	1136796864	pertamina	Pertamina	SPBU Pertamina 44.592.08	-6.7338099	111.3546503	jawa
way	1137788807	pertamina	Pertamina	Pertamina - 34.12307	-6.2784454	106.7727697	jawa
way	1137788815	pertamina	Pertamina	Pertamina - 34.12412	-6.2927842	106.7943361	jawa
way	1138421053	pertamina	Pertamina	SPBU 44.592.04	-6.6899792	111.4217722	jawa
way	1138563903	pertamina	SPBU Pertamina 44.581.09 Kradenan	SPBU Pertamina 44.581.09 Kradenan	-7.1511955	111.1530034	jawa
way	1143843618	pertamina	Pertamina	SPBU Pertamina 31-11804	-6.1308208	106.6879633	sumatra
way	1144241212	pertamina	Pertamina	SPBU 54.633.16	-7.6664471	111.3616354	jawa
way	1144629107	pertamina	Pertamina	SPBU 54.641.15	-7.783668	112.2159536	jawa
way	1149309497	pertamina	Pertashop	Pertashop	-7.4771379	107.2156059	jawa
way	1149443590	shell	Shell	Shell	-6.2741469	106.6130595	jawa
way	1149909581	pertamina	Pertamina	Pertamina	-7.6534119	112.0926852	jawa
way	1150185346	other	\\N	\\N	-8.2509341	114.4788746	jawa
way	1152622066	shell	Shell	Shell	-6.9363637	107.6238456	jawa
way	1153490556	pertamina	Pertamina	SPBU Pertamina 34.127.07	-6.2448557	106.8422291	jawa
way	1155703023	bp	BP	BP	-6.5678367	106.8541362	jawa
way	1156031789	other	\\N	\\N	-7.3244577	108.3294776	jawa
way	1158124101	shell	Shell	Shell	-6.8573452	107.4877735	jawa
way	1158365985	pertamina	Pertamina	Pertamina	-6.9478733	107.6353323	jawa
way	1159072829	shell	Shell	Shell	-6.161357	106.7265706	sumatra
way	1159800569	pertamina	Pertamina	SPBU Pertamina 54.662.14	-8.1275047	112.0151474	jawa
way	1161021081	pertamina	Pertamina	SPBU Pertamina 44.558.03 Gabusan	-7.8859313	110.3524858	jawa
way	1161237336	pertamina	Pertamina	SPBU Pertamina 54.662.28 Podorejo	-8.1159194	111.9525388	jawa
way	1161545647	pertamina	Pertamina	SPBU Pertamina 54.662.16	-8.0912763	111.8996626	jawa
way	1161552945	pertamina	Pertamina	SPBU Pertamina 54.662.18	-8.1356831	111.9601779	jawa
way	1162508105	pertamina	Pertashop Beji	Pertashop Beji	-7.3464556	109.3436346	jawa
way	1163127291	pertamina	SPBU Pertamina 44.592.15	SPBU Pertamina 44.592.15	-6.7057057	111.3640705	jawa
way	1163340983	pertamina	SPBU Pertamina 44.592.10	SPBU Pertamina 44.592.10	-6.7066308	111.3794999	jawa
way	1163660414	pertamina	Pertamina	Pertamina	-6.069061	105.8921541	sumatra
way	1163666440	pertamina	Pertamina	34 - 12602	-6.3186304	106.8376406	jawa
way	1164032128	bp	BP	BP Grand Galaxy	-6.2673701	106.9745174	jawa
way	1173228944	other	pom bensin	pom bensin	-7.1012202	111.9574145	jawa
way	1173407715	pertamina	SPBU Pertamina 54.623.15 Pekuwon Rengel	SPBU Pertamina 54.623.15 Pekuwon Rengel	-7.0818709	111.9757568	jawa
way	1179808166	pertamina	Pertamina	Pertamina	-7.9831646	112.6145894	jawa
way	1180292819	pertamina	Pertamina	SPBU 54.623.02	-6.899759	112.1555211	jawa
way	1182492260	pertamina	VIVO	SPBU VIVO Pekayon	-6.2764232	106.9755208	jawa
way	1182543988	other	\\N	\\N	-6.8975443	112.1688178	jawa
way	1182634029	shell	Shell	Shell	-6.9662961	107.6374528	jawa
way	1184111365	shell	Shell	Shell	-6.9188563	107.5851533	jawa
way	1184118309	shell	Shell	Shell	-6.9437496	107.7564081	jawa
way	1184757526	shell	Shell	Shell	-6.9472785	107.5991364	jawa
way	1184757534	pertamina	Pertamina	Pertamina	-6.9043778	107.6070345	jawa
way	1184767822	shell	Shell	Shell	-6.894576	107.5872113	jawa
way	1184768320	shell	Shell	Shell	-6.8989287	107.604339	jawa
way	1186775237	pertamina	Pertamina	SPBU 34.174.14	-6.2963801	106.9685657	jawa
way	1186776054	pertamina	Pertamina	Pertamina	-7.6712346	112.0437097	jawa
way	1188020113	shell	Shell	Shell	-6.9259288	107.5932695	jawa
way	1188403846	pertamina	Pertamina	SPBU Pertamina 54.641.27	-7.698671	112.214171	jawa
way	1189082378	pertamina	Pertamina	Pertamina	-6.925705	107.6316851	jawa
way	1189166404	shell	Shell	Shell	-7.9779516	112.6240008	jawa
way	1189505544	other	\\N	\\N	-7.5349473	112.5611129	jawa
way	1189529827	pertamina	VIVO	SPBU VIVO Jatikramat	-6.286979	106.9444374	jawa
way	1189534426	other	\\N	\\N	-7.542286	112.5882419	jawa
way	1189777686	pertamina	Pertamina	Pertamina	-6.9134091	107.6338842	jawa
way	1189780777	pertamina	Pertamina	Pertamina	-6.9367444	107.6914537	jawa
way	1190145498	pertamina	Pertamina	Pertamina	-8.3568614	114.630782	jawa
way	1194279853	shell	Shell	Shell Raya Pekayon-1 Bekasi	-6.2867674	106.9674629	jawa
way	1196742587	pertamina	Pertamina	SPBU Pertamina 44.574.33 Bowan	-7.649632	110.6844578	jawa
way	1197333409	pertamina	Pertamina	Pertamina 34-17130	-6.1765627	106.9993633	sumatra
way	1198258366	pertamina	Pertamina	Pertamina 34-17123	-6.1854974	106.9775168	sumatra
way	1201764230	pertamina	Pertamina	Pertamina	-7.788376	112.0004791	jawa
way	1202302311	other	Gudang Garam	\\N	-7.805967	112.0222564	jawa
way	1202329081	pertamina	Pertamina	Pertamina	-7.8131094	112.0244522	jawa
way	1207349667	bp	BP	BP AKR Kota Harapan Indah	-6.1696213	106.9742197	sumatra
way	1207433117	pertamina	VIVO	SPBU VIVO Margajaya	-6.2501479	106.9923156	jawa
way	1210466149	pertamina	SPBU Pertamina	SPBU Banjarsari	-7.5406185	110.8272404	jawa
way	1210492118	bp	BP	BP Gunung Putri	-6.346326	106.9621863	jawa
way	1210492120	pertamina	Pertamina	Pertamina 34-16928	-6.3474967	106.961684	jawa
way	1210494351	pertamina	Pertamina	Pertamina 34-16914	-6.3756591	106.9453324	jawa
way	1210494352	pertamina	Pertamina	Pertamina 31.B.968.01	-6.376086	106.9448598	jawa
way	1210742333	pertamina	Pertamina	SPBU 5464410 Godean	-7.6506251	111.9016831	jawa
way	1210744554	pertamina	Pertamina	Pertamina Candi Nganjuk 54.644.04	-7.6394522	111.8940785	jawa
way	1210751950	pertamina	Pertamina	SPBU Pertamina 54.644.03	-7.6007591	111.9202978	jawa
way	1210758036	pertamina	Pertamina	Pertamina	-7.5905062	112.1320242	jawa
way	1210758037	pertamina	Pertamina	Pertamina	-7.5965949	112.1185851	jawa
way	1210808941	pertamina	Pertamina	Pertamina	-6.9402951	107.677419	jawa
way	1211157319	pertamina	Pertamina	SPBU Pertamina 54.644.16	-7.6869805	111.9276696	jawa
way	1211160667	pertamina	Pertamina	Pertamina	-7.5833649	111.9174239	jawa
way	1211251278	pertamina	Pertamina	Pertamina	-7.7782044	112.0166775	jawa
way	1211471253	pertamina	Pertamina	Pertamina	-7.8033358	112.0086376	jawa
way	1211951354	pertamina	SPBU AGLIK	SPBU AGLIK	-7.8083333	109.9056551	jawa
way	1212024385	pertamina	Pertamina	Pertamina	-7.8289722	112.0104819	jawa
way	1212216646	pertamina	Pertamina Baron	Pertamina Baron	-7.606412	112.0554258	jawa
way	1212221198	pertamina	Pertamina	SPBU Pertamina 54.641.40	-7.8323658	111.9947462	jawa
way	1212222859	pertamina	Pertamina	SPBU Pertamina 54.641.42	-7.8658403	112.028745	jawa
way	1212467590	pertamina	Pertamina	SPBU 54.641.14 Kandat	-7.906052	112.034512	jawa
way	1212471498	pertamina	Pertamina	SPBU 5464132 KANIGORO	-7.9583281	111.9873609	jawa
way	1212472416	pertamina	Pertamina	SPBU Pertamina 54.641.18 Kras	-7.9606171	111.9555874	jawa
way	1212474357	pertamina	Pertamina	SPBU Pertamina 54.661.113	-7.9970833	112.0366332	jawa
way	1212482841	pertamina	Pertamina	Pertamina	-8.059593	112.0536629	jawa
way	1212640183	pertamina	Pertamina	SPBU Pertamina 54.641.29	-7.7567108	112.0990164	jawa
way	1212641693	pertamina	Pertamina	SPBU Pertamina 54.641.46	-7.7344453	112.1480456	jawa
way	1212642197	pertamina	Pertamina	SPBU Pertamina 54.641.09	-7.7622631	112.171225	jawa
way	1212646383	pertamina	Pertamina	SPBU Pertamina 54.641.02	-7.7631526	112.2880473	jawa
way	1212917295	pertamina	Pertamina	SPBU Pertamina 54.614.08	-7.6892327	112.2629552	jawa
way	1212917742	pertamina	Pertamina	SPBU Pertamina 54.614.30	-7.6781095	112.272333	jawa
way	1212918118	pertamina	Pertamina	SPBU Pertamina 54.614.16	-7.6458632	112.2946383	jawa
way	1212918666	pertamina	Pertamina	SPBU Pertamina 54.614.25	-7.6028789	112.3118843	jawa
way	1212919403	pertamina	Pertamina	SPBU Pertamina 54.614.14	-7.5887184	112.2514745	jawa
way	1212923987	pertamina	Pertamina	SPBU Pertamina 54.661.48	-8.045377	112.2939737	jawa
way	1212942212	other	Mobil	Mobil	-7.5957129	110.9249051	jawa
way	1212976321	pertamina	Pertamina	SPBU Pertamina 54.661.40	-8.0399566	112.2458012	jawa
way	1212981930	pertamina	Pertamina	SPBU Pertamina 54.661.24	-8.0507635	112.2102622	jawa
way	1212988626	pertamina	Pertamina	SPBU Pertamina 54.661.45	-8.1321851	112.1702892	jawa
way	1213173565	pertamina	Pertamina	SPBU Pertamina 54.661.14	-8.1633546	112.1412999	jawa
way	1213175256	pertamina	Pertamina	SPBU Pertamina 54.661.37	-8.1882549	112.1036841	jawa
way	1213292140	pertamina	Pertamina	SPBU Pertamina 54.662.26	-8.0667948	111.8767196	jawa
way	1213294042	pertamina	Pertamina	SPBU Pertamina 54.662.23	-8.0590165	111.8610704	jawa
way	1213500411	pertamina	Pertamina	SPBU Pertamina 54.641.21	-7.8351286	112.1769884	jawa
way	1215285823	pertamina	Pertamina	Pertamina	-6.9359453	107.714289	jawa
way	1216241778	pertamina	Pertamina	SPBU Pertamina 54.662.09	-8.171053	111.7752361	jawa
way	1216246331	pertamina	Pertamina	SPBU Pertamina 54.662.33	-8.1954414	111.7874938	jawa
way	1217025224	pertamina	Pertamina	SPBU Ketapang	-5.7403084	105.7843459	sumatra
way	1217324324	pertamina	Pertamina	SPBU Pertamina 54.663.05	-8.1518922	111.6864085	jawa
way	1217771835	pertamina	Pertamina	SPBU Pertamina Kembangbahu	-7.2246259	112.3708015	jawa
way	1218996111	pertamina	Pertamina	SPBU Pertamina 54.663.04	-8.0483936	111.7170648	jawa
way	1219527474	pertamina	Pertamina	SPBU Pertamina 54.663.13	-8.0775071	111.7031998	jawa
way	1219531000	pertamina	Pertamina	SPBU Pertamina 54.663.11	-8.182408	111.5778609	jawa
way	1221228366	pertamina	pertamina	SPBU Pertamina 54.682.01	-7.9387773	113.8125502	jawa
way	1221230244	pertamina	Pertamina	SPBU Pertamina 54.682.04 Maesan	-8.0345353	113.776052	jawa
way	1221237104	pertamina	pertamina	SPBU Pertamina Baratan	-8.1350183	113.7377463	jawa
way	1221237317	pertamina	pertamina	SPBU Pertamina Arjasa	-8.1161247	113.7487757	jawa
way	1221239310	pertamina	pertamina	SPBU Pertamina Kranjingan	-8.1559496	113.7007162	jawa
way	1221974597	pertamina	Pertamina	SPBU Pertamina 55.663.14	-8.2529464	111.453907	jawa
way	1221984105	pertamina	Pertamina	SPBU Pertamina 54.635.04	-8.2104408	111.3259461	jawa
way	1221985966	pertamina	Pertamina	SPBU Pertamina 54.635.07	-8.2468128	111.2634231	jawa
way	1222229596	pertamina	Pertamina	SPBU Sindang Laut	-6.8295425	108.6200014	jawa
way	1224912763	pertamina	Indomobil	SPBU Mikro	-6.8517132	108.5965781	jawa
way	1227685849	pertamina	Pertamina	SPBU 44.592.25	-6.7053964	111.448748	jawa
way	1227733864	pertamina	Pertamina	SPBU Sedong	-6.8599227	108.5882274	jawa
way	1228789767	pertamina	Pertamina	SPBU Pertamina 45.574.32 Krecek	-7.6203713	110.6879388	jawa
way	1232955921	pertamina	VIVO	SPBU VIVO Ciater	-6.3158994	106.7026873	jawa
way	1237760223	shell	Shell	Shell	-6.1975563	106.706393	sumatra
way	1244919774	pertamina	Pertamina	Pertamina	-6.300217	106.8350438	jawa
way	1248976275	shell	Shell	Shell	-6.3876522	106.9414589	jawa
way	1249082488	pertamina	Pertamina	Pertamina	-6.2757056	106.6952957	jawa
way	1253841899	other	\\N	\\N	-6.3893906	107.4206804	jawa
way	1255453809	other	\\N	\\N	-7.549769	110.8774409	jawa
way	1255477481	shell	Shell	Shell Jatimekar-1	-6.28634	106.9460942	jawa
way	1255482556	shell	Shell	Shell Wibawa Mukti-1 BKS	-6.3086122	106.9537876	jawa
way	1255482560	shell	Shell	Shell Cipendawa-1 BKS	-6.2999915	106.9761127	jawa
way	1255482569	shell	Shell	Shell Cut Mutia-2 BEKASI	-6.2605758	107.0025478	jawa
way	1255482573	shell	Shell	Shell Noer Ali-2	-6.2499043	106.9713286	jawa
way	1255482580	shell	Shell	Shell Pondok Gede-1 BKS	-6.2694746	106.911986	jawa
way	1259688152	pertamina	Pertamina	Pertamina	-6.9319688	107.7753635	jawa
way	1266035755	bp	BP	BP Sentul City	-6.567865	106.8540938	jawa
way	1266108840	pertamina	SPBU Pertamina 44.595.01	SPBU Pertamina 44.595.01	-6.8769933	110.6946659	jawa
way	1266483988	pertamina	Pertamina	SPBU Pertamina 44.574.29 Trucuk	-7.7059315	110.6592836	jawa
way	1267444478	pertamina	Pertamina	Pertamina	-7.052096	107.7100578	jawa
way	1270389089	pertamina	Pertamina	Pertamina	-7.2134564	107.8464123	jawa
way	1272163620	pertamina	Pertamina	Pertamina	-6.3839895	105.9039319	jawa
way	1273090461	pertamina	Pertamina	Pertamina	-7.0764109	107.4870349	jawa
way	1273117220	pertamina	Pertamina	Pertamina	-7.0899582	107.4802215	jawa
way	1273237444	pertamina	Pertamina	SPBU Kanci	-6.7836584	108.6245462	jawa
way	1274403582	pertamina	SPBU Wringinharjo 44.532.08	SPBU Wringinharjo 44.532.08	-7.4863309	108.8189855	jawa
way	1274761563	pertamina	Pertamina	Pertamina	-7.1830234	107.561459	jawa
way	1274849094	pertamina	Pertashop	Pertashop	-7.192728	107.5847097	jawa
way	1275232792	shell	shell	shell	-6.572888	106.7420241	jawa
way	1276123940	other	\\N	\\N	-7.308308	107.7955804	jawa
way	1276339004	pertamina	Pertamina	Pertamina	-7.3494562	107.8015014	jawa
way	1276758935	pertamina	Pertamina	Pertamina	-7.382331	107.8286118	jawa
way	1281163495	pertamina	Pertashop	Pertashop	-7.409218	107.0139625	jawa
way	1281764953	pertamina	Pertamina	Pertamina	-7.4608401	107.1546063	jawa
way	1282342116	pertamina	Pertamina	Pertamina	-7.2934107	107.1255867	jawa
way	1283143303	pertamina	Pertamina	Pertamina	-7.2028273	107.1430907	jawa
way	1283219621	pertamina	Pertamina	Pertamina	-6.8753644	107.5969678	jawa
way	1283656881	pertamina	Pertamina	Pertamina	-7.0924063	107.1339851	jawa
way	1284386748	pertamina	Pertamina	Pertamina	-6.9449971	107.1349197	jawa
way	1284430581	pertamina	Pertamina	Pertamina	-6.9297668	107.1215932	jawa
way	1285024820	pertamina	Pertamina	Pertamina	-6.9170403	107.124093	jawa
way	1285218615	pertamina	Pertamina	Pertamina	-6.8828875	107.125615	jawa
way	1285459959	pertamina	Pertamina	Pertamina	-6.869683	107.1271545	jawa
way	1285514140	pertamina	Pertamina	Pertamina	-6.8474598	107.1360232	jawa
way	1286145249	pertamina	Pertamina	Pertamina	-6.835623	107.1401035	jawa
way	1286204032	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5694393	106.735326	jawa
way	1286469888	pertamina	Pertamina	Pertamina	-6.7751922	107.1857051	jawa
way	1286504766	other	Mobil	Mobil	-6.7696092	107.1916116	jawa
way	1286568795	pertamina	Pertamina	Pertamina	-6.5692161	106.7569611	jawa
way	1286575075	pertamina	Pertamina	Pertamina	-6.7232448	107.2120189	jawa
way	1286996854	pertamina	SPBU Pertamina	SPBU Pertamina	-6.6016658	106.7525049	jawa
way	1286998837	pertamina	SPBU Pertamina	SPBU Pertamina	-6.606803	106.7395788	jawa
way	1287229959	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5380941	106.7250773	jawa
way	1287343766	pertamina	Pertamina	Pertamina	-6.5583635	106.7115838	jawa
way	1287592706	pertamina	Pertamina	Pertamina	-6.6956829	111.2895501	jawa
way	1287600824	shell	Shell	Shell	-6.5567397	106.7736762	jawa
way	1287600825	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5567769	106.7734855	jawa
way	1287608256	pertamina	SPBU BP AKR	SPBU BP AKR	-6.5565516	106.7793027	jawa
way	1288239541	shell	Shell	Shell	-6.588485	106.7847557	jawa
way	1289755298	pertamina	VIVO	SPBU VIVO Bogor	-6.5808281	106.7973592	jawa
way	1290897543	pertamina	Pertamina	SPBU Benteng	-7.3428244	108.3460285	jawa
way	1291943202	pertamina	SPBU Pertamina	\\N	-6.6730532	106.9279297	jawa
way	1292529024	pertamina	SPBU Pertamina	SPBU Pertamina	-6.5623561	106.6838115	jawa
way	1292529925	pertamina	SPBU Pertamina	SPBU Pertamina	-6.564499	106.6776244	jawa
way	1293008730	pertamina	SPBU Pertamina	SPBU Pertamina	-6.6316794	106.8251452	jawa
way	1293528265	pertamina	SPBU Pertamina	SPBU Pertamina	-6.9302936	107.6037514	jawa
way	1294572465	shell	Shell	Shell	-6.0466591	106.6958492	sumatra
way	1295604632	pertamina	Pertamina	Pertashop 5P.651.62 - MRV	-7.9722034	112.5851634	jawa
way	1298328993	pertamina	spbu	spbu	-6.6951446	111.339191	jawa
way	1300891693	pertamina	Pertashop	Pertashop 5P	-7.7821368	110.5340244	jawa
way	1301659221	pertamina	SPBU 34-11706	SPBU 34-11706	-6.1525923	106.719308	sumatra
way	1302200264	pertamina	Pertamina	Pertamina 34.17101	-6.2863706	106.9865597	jawa
way	1305357248	pertamina	Pertamina	Pertamina	-6.1551041	106.5319741	sumatra
way	1306241578	pertamina	Pertamina	Pertamina	-6.9239028	107.6966157	jawa
way	1306275790	pertamina	SPBU Pertamina	SPBU Pertamina 44.562.11 Kledung	-7.3345338	110.0373438	jawa
way	1307388672	shell	Shell	Shell	-6.3065527	107.3064648	jawa
way	1307893546	pertamina	Pertamina	SPBU Pertamina 44.556.01 Sentolo	-7.8260103	110.2258878	jawa
way	1308464344	shell	Shell	Shell	-6.3586436	107.1817223	jawa
way	1308464349	pertamina	Pertamina	Pertamina 34-17535	-6.3537776	107.1823237	jawa
way	1313099904	pertamina	Pertamina	SPBU Pertamina 53.623.21 Patung	-6.8944671	112.0393021	jawa
way	1313397408	pertamina	Pertamina	Pertamina	-6.7742313	107.0640293	jawa
way	1313874149	pertamina	Pertamina	Pertamina	-6.7102632	107.0597348	jawa
way	1314298009	shell	Shell	Shell	-6.3466027	106.700855	jawa
way	1316553700	pertamina	Pertashop	Pertashop	-7.1762114	107.6844216	jawa
way	1316833290	pertamina	SPBU CIHAUR 34.454.13	SPBU CIHAUR 34.454.13	-6.9378978	108.3042216	jawa
way	1316900007	pertamina	Pertamina	SPBU Pertamina 44.574.19 Kebonalas	-7.6944127	110.4888274	jawa
way	1317039062	pertamina	SPBU Pertamina	SPBU PERTAMINA 44.573.18 SUDIMORO	-7.5566497	110.6396016	jawa
way	1317288263	pertamina	Pertamina	SPBU Pertamina 44.574.27 Wonorejo	-7.6403371	110.761917	jawa
way	1317292024	pertamina	Pertamina	Pertamina 34-13810	-6.3088621	106.8970396	jawa
way	1317292025	vivo	VIVO	VIVO HANKAM	-6.3089422	106.8973261	jawa
way	1317669888	other	PTTE CNG	SPBG Terminal Pinang Ranti	-6.2911944	106.8867255	jawa
way	1317971431	pertamina	Vivo	SPBU VIVO Cilangkap	-6.3387215	106.9003784	jawa
way	1317971432	pertamina	VIVO	SPBU VIVO Pasar Minggu	-6.2513925	106.8431357	jawa
way	1317971433	pertamina	VIVO	SPBU VIVO MT Haryono	-6.2427106	106.8497737	jawa
way	1317971434	pertamina	VIVO	SPBU VIVO Kalimalang	-6.2492787	106.9522082	jawa
way	1317971437	pertamina	VIVO	SPBU VIVO Warung Buncit	-6.2642581	106.8300038	jawa
way	1317971440	pertamina	VIVO	SPBU VIVO Pejaten	-6.2846149	106.8269991	jawa
way	1317971442	pertamina	VIVO	SPBU VIVO Kemang	-6.2610932	106.8153221	jawa
way	1317971443	pertamina	VIVO	SPBU VIVO Cilandak	-6.3096806	106.8134857	jawa
way	1317971447	pertamina	VIVO	SPBU VIVO Antasari	-6.2779099	106.8079645	jawa
way	1317971450	pertamina	VIVO	SPBU VIVO Tendean	-6.2391297	106.8187414	jawa
way	1317971451	pertamina	VIVO	SPBU VIVO Radar Auri	-6.3694281	106.8871018	jawa
way	1317971454	pertamina	VIVO	SPBU VIVO Cimuning	-6.3210977	107.0193292	jawa
way	1317971455	pertamina	VIVO	SPBU VIVO Cideng	-6.1665909	106.8110281	sumatra
way	1317971456	pertamina	VIVO	SPBU VIVO Bintaro 1	-6.26929	106.7545856	jawa
way	1317971457	pertamina	VIVO	SPBU VIVO Kedoya	-6.1749057	106.7654406	sumatra
way	1317971458	pertamina	VIVO	SPBU VIVO Pamulang	-6.3398618	106.7390118	jawa
way	1317971460	pertamina	VIVO	SPBU VIVO Tambun	-6.2655283	107.0744951	jawa
way	1318056592	vivo	VIVO	Vivo	-6.2874691	106.6658338	jawa
way	1318056593	pertamina	VIVO	SPBU VIVO Limo	-6.3666269	106.7745183	jawa
way	1318056594	pertamina	VIVO	SPBU VIVO Pajaran	-6.5895862	106.805094	jawa
way	1318659798	pertamina	Pertamina	Pertashop 5P.651.37	-7.9263574	112.5821265	jawa
way	1318751386	shell	Shell	Shell Cut Meutia-1 BKS	-6.2722629	106.992965	jawa
way	1318751391	shell	Shell	Shell Pemuda-1 EAST JKT	-6.1938345	106.8889863	sumatra
way	1318751392	shell	Shell	Shell A.Yani-1 CNTRL JKT	-6.1859387	106.8748202	sumatra
way	1318751395	shell	Shell	Shell Suprapto-1 CENTRAL JKT	-6.1705651	106.864834	sumatra
way	1318751397	shell	Shell	Shell	-6.1480557	106.8887998	sumatra
way	1318751399	shell	Shell	Shell Kelapa Gading-1 NORTH JKT DO	-6.1580128	106.9003958	sumatra
way	1318751401	shell	Shell	Shell Semper-1 JKT DO	-6.1309606	106.9142054	sumatra
way	1318966792	shell	Shell	Shell Sunter Sel-1 NORTH JKT	-6.1469012	106.8669658	sumatra
way	1318966794	shell	Shell	Shell Kemayoran-1 CENTRAL JKT	-6.1463807	106.8355428	sumatra
way	1318966796	shell	Shell	Shell GN. Sahari-1 CENTRAL JKT	-6.160663	106.8381547	sumatra
way	1318966798	shell	Shell	Shell Menteng-1 CENTRAL JKT	-6.1852271	106.8360374	sumatra
way	1318966800	shell	Shell	Shell RP Soeroso-1 CENTRAL JKT	-6.1906928	106.8363377	sumatra
way	1318966802	shell	Shell	Shell Satrio-1 SOUTH JKT	-6.2199894	106.8205026	jawa
way	1318966804	shell	Shell	Shell Lapangan Ros-1 SOUTH JAKARTA	-6.2248586	106.8526602	jawa
way	1318966806	shell	Shell	Shell MT Haryono-1 EAST JKT	-6.2438546	106.864455	jawa
way	1318966808	shell	Shell	Shell MT Haryono-2 SOUTH JKT	-6.2425238	106.8555554	jawa
way	1318966810	shell	Shell	Shell Pasar Minggu-1 SOUTH JKT	-6.2642951	106.8429677	jawa
way	1318966812	shell	Shell	Shell Kemang Raya-1 SOUTH JKT	-6.2608359	106.8158174	jawa
way	1318966814	shell	Shell	Shell Antasari-2 SOUTH JKT	-6.2654835	106.8081619	jawa
way	1318966819	shell	Shell	Shell Fatmawati-2 SOUTH JKT	-6.273676	106.7973964	jawa
way	1318966824	shell	Shell	Shell Warung JT BRT-1 SOUTH JKT	-6.2837851	106.8265174	jawa
way	1319011501	bp	BP	BP	-7.938297	112.6284136	jawa
way	1319276354	shell	Shell	Shell	-6.3039354	106.8403545	jawa
way	1319276355	shell	Shell	Shell Ciputat Raya-1 SOUTH JKT	-6.2860426	106.7716326	jawa
way	1319276357	shell	Shell	Shell Veteran-1 SOUTH JKT	-6.2654807	106.7658717	jawa
way	1319276360	shell	Shell	Shell Arteri PI-1 SOUTH JKT	-6.259479	106.7813934	jawa
way	1319276363	shell	Shell	Shell Radio Dalam-1 SOUTH JKT	-6.2602806	106.7892796	jawa
way	1319276365	shell	Shell	Shell Ciputat Raya-2 SOUTH JKT	-6.2508282	106.7785648	jawa
way	1319276368	shell	Shell	Shell Pos Pengumben-2 WEST JKT	-6.2206439	106.7406141	jawa
way	1319706037	shell	Shell	Shell Pos Pengumben-1 WEST JKT	-6.2164381	106.7666161	jawa
way	1319706043	shell	Shell	Shell Petukangan-1 SOUTH JAKARTA	-6.2377541	106.7598488	jawa
way	1319706046	shell	Shell	Shell Tentara Pelajar-1 SOUTH JKT	-6.2146014	106.794516	jawa
way	1319706051	shell	Shell	Shell Gatot Subroto-1 SOUTH JKT	-6.2408049	106.8371626	jawa
way	1319706053	shell	Shell	Shell Mampang-1 SOUTH JKT	-6.2464422	106.8264719	jawa
way	1319706055	shell	Shell	Shell Tendean-1 SOUTH JKT	-6.2396127	106.8194371	jawa
way	1319706056	shell	Shell	Shel Hasyimashari-1 CENTRAL JKT	-6.1651152	106.8125908	sumatra
way	1319706058	shell	Shell	Shell Kyai Tapa-1 WEST JKT	-6.1668333	106.7965288	sumatra
way	1319706061	shell	Shell	Shell Latumenten-2 WEST JKT	-6.1594238	106.7906763	sumatra
way	1319706062	shell	Shell	Shell Kemanggisan-1 WEST JKT	-6.189552	106.7926198	sumatra
way	1319706064	shell	Shell	Shell S Parman-1 WEST JKT	-6.1986533	106.7976191	sumatra
way	1319951260	pertamina	Pertamina	Pertashop	-7.9397349	112.5933608	jawa
way	1319978742	pertamina	Pertamina	SPBU Pertamina COCO 31.131.01	-6.1933516	106.864057	sumatra
way	1319978743	pertamina	Pertamina	SPBU Pertamina COCO 31.133.01	-6.2311809	106.8676847	jawa
way	1319978747	pertamina	Pertamina	SPBU Pertamina COCO 31.134.01	-6.2123809	106.9002541	jawa
way	1319978749	pertamina	Pertamina	SPBU Pertamina COCO 31.134.02	-6.247419	106.9180564	jawa
way	1319978753	pertamina	Pertamina	SPBU Pertamina COCO 31.136.02	-6.2601128	106.8677925	jawa
way	1319978754	pertamina	Pertamina	SPBU Pertamina COCO 31.137.01	-6.3530347	106.8624673	jawa
way	1320234217	pertamina	Pertamina	SPBU Pertamina COCO 31.139.02	-6.1829455	106.9146841	sumatra
way	1320234220	pertamina	Pertamina	SPBU Pertamina 33-13402	-6.2289272	106.9125095	jawa
way	1320234229	pertamina	Pertamina	SPBU Pertamina 33-13401	-6.2491014	106.9412821	jawa
way	1320234230	pertamina	Pertamina	SPBU Pertamina 34-13101	-6.1961728	106.8737161	sumatra
way	1320234231	pertamina	Pertamina	SPBU Pertamina 34-13808	-6.3322361	106.8887359	jawa
way	1320234233	pertamina	Pertamina	SPBU Pertamina 34-13805	-6.3059402	106.9167028	jawa
way	1320234237	pertamina	Pertamina	SPBU Pertamina 34-13503	-6.2950019	106.8821971	jawa
way	1320234239	pertamina	Pertamina	SPBU Pertamina 34-13504	-6.2906362	106.8844977	jawa
way	1320234241	pertamina	Pertamina	SPBU Pertamina 34-13102	-6.2026093	106.8561736	jawa
way	1320234242	pertamina	Pertamina	Pertamina	-6.2050449	106.8574335	jawa
way	1321394920	pertamina	Pertashop	Pertashop	-7.2686876	106.8338757	jawa
way	1321521663	pertamina	Pertamina	Pertamina	-6.5089497	105.7830995	jawa
way	1321522515	pertamina	Pertamina	Pertamina	-6.5369227	105.7569807	jawa
way	1321538874	pertamina	Pertamina	Pertamina	-6.5246978	105.7394959	jawa
way	1321540651	pertamina	Pertamina	Pertamina	-6.6030452	105.6961301	jawa
way	1321542978	pertamina	Pertamina	Pertamina	-6.6642048	105.5856208	jawa
way	1321543933	pertamina	Pertamina	Pertamina	-6.7104534	105.6985428	jawa
way	1321544117	pertamina	Pertamina	Pertamina	-6.763937	105.8591373	jawa
way	1321545109	pertamina	Pertamina	Pertamina	-6.5037955	105.9832794	jawa
way	1321546082	pertamina	Pertamina	Pertamina	-6.312277	105.9961211	jawa
way	1322067378	pertamina	Pertamina	Pertamina	-7.9781106	112.6454044	jawa
way	1323732336	pertamina	Pertamina	Pertashop Teguhan	-7.4299305	111.0337049	jawa
way	1324964717	other	\\N	\\N	-6.2134973	107.0337947	jawa
way	1334991964	shell	Shell	Shell	-6.2740031	106.5249861	jawa
way	1338168887	pertamina	Pertamina	Pertamina	-7.3269474	110.4791846	jawa
way	1338168888	pertamina	Pertamina	Pertamina	-7.3369754	110.4773451	jawa
way	1338985960	other	\\N	\\N	-7.4606807	112.7231891	jawa
way	1340741507	bp	BP	BP/AKR	-6.9475725	107.6325729	jawa
way	1341143068	other	\\N	\\N	-6.2221666	107.0333871	jawa
way	1341971665	pertamina	Pertamina	Pertamina	-7.7389456	110.3625969	jawa
way	1342127250	pertamina	Pertamina	Pertamina 34-17553	-6.2215581	107.1815179	jawa
way	1342127252	pertamina	Pertamina	Pertamina 34-17601	-6.2319662	107.1599323	jawa
way	1345303785	pertamina	Pertamina	SPBU Pertamina 44.574.17 Jebugan	-7.6789194	110.6034241	jawa
way	1348475779	pertamina	SPBU Pertamina	SPBU Ngangkruk 44.574.19	-7.5810117	110.5944741	jawa
way	1352073843	other	\\N	\\N	-7.0968977	110.3819417	jawa
way	1352268535	pertamina	Pertamina	Pertamina UNNES	-7.049043	110.3975925	jawa
way	1354094109	pertamina	Pertamina	SPBU Pertamina 44.552.22 Trihanggo	-7.7550422	110.3382619	jawa
way	1354094111	pertamina	Pertamina	SPBU Pertamina 45.557.17 Terong	-7.885709	110.451889	jawa
way	1354125543	pertamina	Pertamina	SPBU Pertamina Nyaen Pendowoharjo	-7.6907425	110.368831	jawa
way	1354125544	pertamina	Pertamina	SPBU Pertamina 44.555.39 Sardonoharjo	-7.7017845	110.4063952	jawa
way	1354125545	pertamina	Pertamina	SPBU Pertamina 44.552.21 Godean	-7.7663295	110.289412	jawa
way	1354125546	pertamina	Pertamina	SPBU Pertamina 44.555.37 Minggir	-7.7208609	110.2393071	jawa
way	1354128976	pertamina	Pertamina	SPBU Pertamina 44.556.15 Demangrejo	-7.8812282	110.2064805	jawa
way	1354128977	pertamina	Pertamina	SPBU Pertamina 44.556.17 Nanggulan	-7.7961565	110.2283608	jawa
way	1354128978	pertamina	Pertamina	SPBU Pertamina 44.556.19 Kalibawang	-7.6516901	110.2564847	jawa
way	1354130577	pertamina	Pertamina	SPBU Pertamina 44.557.18 Mangkuluhur	-7.9018891	110.3453045	jawa
way	1354130578	pertamina	Pertamina	SPBU Pertamina 44.551.22 Bakulan	-7.9116951	110.3631955	jawa
way	1354331037	pertamina	Pertamina	SPBU Pertamina 44.551.21 Bibis	-7.8291757	110.3190555	jawa
way	1354335890	pertamina	Pertamina	SPBU Pertamina 45.574.36 Pomah	-7.61701	110.5863392	jawa
way	1354335891	pertamina	Pertamina	SPBU Pertamina 45.574.39 Padas	-7.6425023	110.6125365	jawa
way	1354534361	pertamina	Pertamina	Pertamina	-7.6731346	110.7104596	jawa
way	1354881937	pertamina	Pertamina	Pertamina	-6.3439053	106.6869811	jawa
way	1355080383	pertamina	Pertamina	Pertamina	-6.2533071	106.5609582	jawa
way	1355907518	pertamina	Pertamina	SPBU Pertamina 44.581.23 Tegowanu	-7.0494274	110.601024	jawa
way	1359379073	bp	BP	BP/AKR	-6.8839109	107.6137973	jawa
way	1359806462	pertamina	Pertamina	Pertamina	-7.9571163	112.6133335	jawa
way	1360213674	pertamina	Pertamina	Pertamina 34.176.06	-6.2007965	107.1301379	jawa
way	1361089431	shell	Shell	Shell	-6.5653451	106.8458242	jawa
way	1362135848	pertamina	Pertamina	pertamina	-6.7063862	111.0602751	jawa
way	1362135888	pertamina	Pertamina	Pertamina	-6.6898027	111.0702222	jawa
way	1362325235	other	Mobil	Mobil	-6.7078132	111.0264478	jawa
way	1362325274	pertamina	Pertamina	Pertamina	-6.6691047	111.0714445	jawa
way	1363581843	pertamina	SPBU Pertamina	SPBU Pertamina	-7.3235626	108.229496	jawa
way	1366103845	pertamina	Pertamina	Pertamina 34-17546	-6.3266987	107.1824772	jawa
way	1370770431	shell	Shell	Shell	-6.1834055	106.5109398	sumatra
way	1370770432	pertamina	Pertamina	Pertamina	-6.1847622	106.5106421	sumatra
way	1371603548	pertamina	Pertamina	Pertamina 34-41335	-6.3960743	107.4645627	jawa
way	1374646132	pertamina	SPBU Pansela Suwuk	SPBU Pansela Suwuk	-7.7572242	109.4799996	jawa
way	1374676886	pertamina	Pertamina	Pertamina	-7.1079132	107.4542916	jawa
way	1375332185	bp	BP	BP	-6.3384234	107.1533007	jawa
way	1375332203	pertamina	Pertamina	Pertamina	-6.343314	107.1551112	jawa
way	1378352203	pertamina	Pertamina	Pertamina	-6.1628532	107.0058273	jawa
way	1378391910	pertamina	Pertamina	Pertamina	-7.9333332	112.6585586	jawa
way	1379018819	pertamina	Pertamina	Pertamina	-6.3867603	107.0162954	jawa
way	1379209539	pertamina	Pertamina	Pertamina	-6.4114081	107.0099463	jawa
way	1379215148	pertamina	Pertamina	Pertamina	-6.4163911	107.0251933	jawa
way	1382568122	pertamina	Pertamina	SPBU Pertamina 34.16301	-6.5074433	106.757868	jawa
way	1382942460	pertamina	Pertamina	Pertamina	-6.330931	107.1480985	jawa
way	1383621323	pertamina	Pertamina	Pertamina	-8.0374545	112.6427362	jawa
way	1386543274	bp	BP	BP	-6.2108952	106.6293175	jawa
way	1386543281	pertamina	Pertamina	Pertamina	-6.2165177	106.6315203	jawa
way	1386543282	pertamina	Pertamina	Pertamina	-6.2175896	106.6313896	jawa
way	1386634061	pertamina	Pertamina	Pertamina	-6.2239615	106.6369534	jawa
way	1386653551	shell	Shell	Shell	-6.2181073	106.6173973	jawa
way	1387289246	pertamina	Pertamina	SPBU PucangLaban	-8.1906033	112.0204833	jawa
way	1391807013	pertamina	Pertamina	SPBU 54.661.35 Gunung Betet	-8.1669469	112.2036966	jawa
way	1393404843	pertamina	Pertamina	Pertamina	-6.3218413	106.7467795	jawa
way	1393404860	pertamina	Pertamina	Pertamina	-6.3210729	106.7458305	jawa
way	1393458015	pertamina	Pertamina	Pertamina	-6.4995748	106.77246	jawa
way	1394084037	pertamina	Pertamina	SPBU Pertamina 44.595.14 Mijen	-7.0299367	110.7226451	jawa
way	1395955348	pertamina	Pertamina	Pertamina	-6.9562258	107.6554081	jawa
way	1396666335	pertamina	Pertamina	Pertamina	-6.2334801	106.9653017	jawa
way	1396666336	shell	Shell	Shell	-6.2207667	106.963263	jawa
way	1406390401	pertamina	Pertamina	Pertamina	-6.2464258	106.9653857	jawa
way	1409523832	pertamina	Pertamina	SPBU Pertamina 45.521.01 Bengle Talang	-6.9205749	109.1516793	jawa
way	1409678199	bp	BP	BP	-6.0644917	106.6902218	sumatra
way	1410468964	pertamina	Pertamina	Pertamina	-6.2406671	106.6706009	jawa
way	1417579090	shell	Shell	Shell	-6.1759227	106.9561502	sumatra
way	1419371335	other	Mobil	Mobil	-7.4231145	106.7215946	jawa
way	1419979442	pertamina	Pertamina	Pertamina	-7.3329229	106.5756434	jawa
way	1420107432	pertamina	Pertamina	Pertamina	-7.2984624	106.6101889	jawa
way	1420292771	pertamina	Pertamina	Pertamina	-7.5240975	107.4860984	jawa
way	1420692487	pertamina	Pertamina	Pertamina	-7.9207396	112.5951122	jawa
way	1421986729	pertamina	Pertamina	SPBU Pertamina	-6.320599	107.3241651	jawa
way	1422201495	other	Mobil	Mobil	-7.4882844	107.3565534	jawa
way	1422201881	pertamina	Pertamina	Pertamina	-7.6442697	107.7208055	jawa
way	1422295284	pertamina	Pertamina	Pertamina	-7.9619258	112.624002	jawa
way	1422300569	pertamina	Pertamina	Pertamina	-6.6967527	108.1829276	jawa
way	1422947823	pertamina	Pertamina	Pertamina	-6.9510459	106.9131967	jawa
way	1422948861	pertamina	Pertamina	Pertamina	-6.960739	106.8878883	jawa
way	1422948972	pertamina	Pertamina	Pertamina - Pertashop	-6.9700455	106.8142206	jawa
way	1423140288	pertamina	Pertamina	SPBU Pertamina 44.576.14 Sambiroto Pracimantoro	-8.0772365	110.8453349	jawa
way	1424029614	vivo	Vivo	Vivo	-6.8962976	107.5911752	jawa
way	1424421419	other	Mobil	Mobil	-7.1595757	106.763807	jawa
way	1424422668	pertamina	Pertashop	Pertashop	-7.0098344	106.9605456	jawa
way	1424423237	other	Mobil	Mobil	-7.018547	106.9534426	jawa
way	1424545961	bp	BP	BP	-6.2881609	106.6176986	jawa
way	1425254125	pertamina	Pertamina	Pertamina	-7.0375018	106.802401	jawa
way	1425254708	other	Mobil	Mobil	-7.0311821	106.8006183	jawa
way	1425811449	pertamina	Pertamina	Pertamina	-6.9536855	106.7645698	jawa
way	1425861502	pertamina	Pertamina	Pertamina	-6.9701314	106.735743	jawa
way	1426270535	other	TNI Angkatan Laut	SPBT Dewaruci	-6.7223963	108.5501363	jawa
way	1427631896	pertamina	Pertamina	Pertamina	-6.1429336	106.5483686	sumatra
way	1427631924	pertamina	Pertamina	Pertamina	-6.1715443	106.5571897	sumatra
way	1427933050	pertamina	Pertamina	SPBU Pertamina 44.572.01 Gemolong	-7.4096784	110.8210424	jawa
way	1429285706	pertamina	Pertamina	Pertamina	-7.9853045	112.6578036	jawa
way	1429285712	pertamina	Pertamina	Pertamina	-7.9817202	112.6541041	jawa
way	1430156490	pertamina	Pertamina	Pertamina	-6.2526082	106.5208841	jawa
way	1430916336	pertamina	Pertamina	Pertamina	-6.9564072	110.4164057	jawa
way	1432189553	pertamina	Pertamina	Pertamina	-6.3414347	106.2403398	jawa
way	1432189579	pertamina	Pertamina	Pertamina	-6.3491586	106.2284522	jawa
way	1432189590	pertamina	Pertamina	Pertamina	-6.3435633	106.2078782	jawa
way	1432472873	pertamina	Pertamina	Pertamina	-6.3559434	106.2775596	jawa
way	1434798107	pertamina	Pertamina	Pertamina	-6.9431313	107.6502653	jawa
way	1441579252	pertamina	Pertamina	SPBU 54.651.74	-7.8505864	112.6945884	jawa
way	1442290563	pertamina	Pertamina	SPBU Bakalan	-7.6697153	112.9178308	jawa
way	1442513904	pertamina	Pertamina	SPBU 54.671.01 Bugul Kidul	-7.648682	112.9184149	jawa
way	1443391670	pertamina	Pertamina	\\N	-7.2057641	108.5021096	jawa
way	1443731463	pertamina	Pertamina	Pertamina	-7.0020804	110.447876	jawa
way	1444315254	pertamina	Pertamina	Pertamina	-6.1216656	106.8043038	sumatra
way	1444680083	pertamina	Pertamina	SPBU Pertamina 34.452.37	-6.3754347	107.957904	jawa
way	1444835986	pertamina	Pertamina	Pertamina	-6.1072403	106.9221567	sumatra
way	1445764397	pertamina	Pertamina	\\N	-7.7738777	110.4103809	jawa
way	1447743524	other	\\N	\\N	-8.1489524	114.6323875	jawa
way	1450074597	pertamina	Pertamina	Pertamina	-7.0893876	112.5813762	jawa
way	1450074605	akr	AKR	AKR	-7.0827266	112.5718162	jawa
way	1450074614	akr	AKR	AKR	-7.0913134	112.6004001	jawa
way	1450074615	akr	AKR	AKR	-7.0918434	112.6019489	jawa
way	1451010803	pertamina	Pertamina	SPBU Pertamina 44.513.03 Tlahab	-6.9378567	110.1270432	jawa
way	1451010804	pertamina	Pertamina	SPBU Pertamina 44.513.12 Gondang Cepiring	-6.9363827	110.1368441	jawa
way	1451010805	pertamina	Pertamina	SPBU Pertamina 44.513.07 Kendal	-6.951278	110.235247	jawa
way	1452011150	pertamina	Pertamina	\\N	-6.3044558	107.2775017	jawa
way	1452017401	pertamina	Pertamina	SPBU Pertamina	-6.2836128	107.3058784	jawa
way	1452020002	pertamina	Pertamina	SPBU Pertamina	-6.2728363	107.3321581	jawa
way	1453236436	pertamina	Pertamina	SPBU 54.612.02	-7.3687782	112.7638589	jawa
way	1453236440	pertamina	Pertamina	SPBU 54.612.16	-7.3806149	112.7616481	jawa
way	1453968984	shell	Shell	Shell	-7.2798546	112.7890389	jawa
way	1456102553	pertamina	Pertamina	SPBU Pertamina 54.653.83 Tlekung	-7.9131227	112.549574	jawa
way	1456601889	pertamina	SBPU Pertamina Karangampel	SBPU Pertamina Karangampel	-6.4613124	108.451555	jawa
way	1461835358	pertamina	Pertamina	SPBU Pertamina 54.651.68	-7.9668794	112.6807283	jawa
way	1462796308	pertamina	Pertamina	Pertamina	-7.9377755	112.627393	jawa
way	1463042309	pertamina	BP	SPBU BP Perusahaan Raya	-7.9137987	112.6453299	jawa
way	1465695768	pertamina	Pertamina	Pertamina	-8.0080691	112.6433425	jawa
way	1466159823	pertamina	Pertamina	SPBU Pertamina 54.651.67	-7.8650031	112.5423168	jawa
way	1468273926	pertamina	Pertamina	Pertamina	-6.8917732	107.5370299	jawa
way	1468274916	pertamina	Pertamina	Pertamina	-6.8928083	107.5389745	jawa
way	1469021219	pertamina	Pertamina	Pertamina	-6.4628048	107.8824129	jawa
way	1470157333	pertamina	Pertamina	SPBU Pertamina 44.571.04 Pajang	-7.5683311	110.7866372	jawa
way	1470906263	pertamina	Pertamina	SPBU Pertamina 41.551.01 Baciro	-7.7905234	110.3801289	jawa
way	1471540256	pertamina	Pertamina	SPBU Krapyak	-6.6008351	110.6650958	jawa
way	1471545609	pertamina	Pertamina	SPBU Ujungbatu	-6.5840221	110.6657223	jawa
way	1471545610	pertamina	Pertamina	SPBU Mulyoharjo	-6.5848747	110.6783669	jawa
way	1471651137	pertamina	Pertamina	SPBU Nalumsari	-6.7578791	110.766307	jawa
way	1472125532	pertamina	pertamina	\\N	-6.8114733	111.4259503	jawa
way	1472417736	pertamina	Pertamina	Pertamina	-7.9662662	112.660921	jawa
way	1472871337	pertamina	Pertamina	SPBU Pertamina 44.577.12 Klodran	-7.5345627	110.7924785	jawa
way	1473275472	pertamina	Pertamina	SPBU Pertamina 44.595.05 Kembangarum	-7.0274882	110.5267001	jawa
way	1474046745	other	\\N	\\N	-7.2275297	112.5000582	jawa
way	1474231401	shell	Shell	Shell	-6.9122038	107.6017538	jawa
way	1474258788	pertamina	Pertamina	SPBU Pertamina 54.651.18	-7.9055333	112.5722812	jawa
way	1474614884	pertamina	Pertamina	Pertamina	-7.6782466	110.8002475	jawa
way	1475197282	pertamina	Pertamina	Pertamina	-6.3804192	106.9228499	jawa
way	1476788201	pertamina	Pertamina	Pertamina	-7.9314014	112.6029406	jawa
way	1477348922	pertamina	Pertamina	SPBU Pertamina 34.45155	-6.6259914	108.4147371	jawa
way	1479087958	pertamina	Pertamina	SPBU 44.563.10 Kejajar	-7.2575694	109.955457	jawa
way	1479091281	pertamina	Pertamina	Pertamina 34-13810	-6.3087954	106.8967823	jawa
way	1482113348	pertamina	SPBU Tambakberas	SPBU Tambakberas	-7.2038197	112.5743464	jawa
way	1483997094	pertamina	Pertamina	Pertamina	-6.8556576	107.4981028	jawa
way	1485370124	pertamina	Pertamina	Pertamina	-7.9352769	112.6493843	jawa
way	1485382856	pertamina	Pertamina	Pertamina	-8.0166257	112.6196757	jawa
way	1485384382	pertamina	Pertamina	Pertamina	-8.0037186	112.6297926	jawa
way	1485384723	pertamina	Pertamina	Pertamina	-8.0196263	112.6283638	jawa
way	1487101797	pertamina	Pertamina	Pertamina	-7.9954096	112.6197413	jawa
way	1487306226	pertamina	Pertamina	Pertamina	-8.0375839	112.639963	jawa
way	1487311522	pertamina	Pertamina	Pertamina	-7.9641793	112.6031091	jawa
way	1487314713	pertamina	Pertamina	Pertamina	-8.0312147	112.6271031	jawa
way	1487321286	pertamina	Pertamina	Pertamina	-7.9806699	112.6374026	jawa
way	1487324359	pertamina	Pertamina	Pertamina	-7.9421196	112.6495285	jawa
way	1487694150	pertamina	Pertamina	Pertamina	-6.8709677	107.5385002	jawa
way	1488742918	pertamina	Pertamina	Pertamina	-6.2585038	107.1662128	jawa
way	1489452281	pertamina	Pertamina	Pertamina	-7.9128214	112.6417718	jawa
way	1491527236	other	Pompa SPBT 52-0604	Pompa SPBT 52-0604	-7.9717091	112.6436874	jawa
way	1492209239	pertamina	Pertamina	Pertamina	-6.3291512	107.0501891	jawa
way	1492209240	pertamina	Pertamina	Pertamina	-6.3149587	107.0624221	jawa
way	1492284830	pertamina	Pertamina	Pertamina	-7.5324838	110.6022169	jawa
way	1493456107	pertamina	ENVOGAS	SPBG CNG Cirebon	-6.7019181	108.4615218	jawa
way	1496909279	bp	bp	bp	-6.3031711	106.78112	jawa
way	1497032901	pertamina	Pertamina	34.12412	-6.292826	106.7943307	jawa
way	1497228126	shell	Shell	Shell Cinere	-6.3275884	106.7853307	jawa
way	1497622762	pertamina	Pertamina	Pertamina	-6.8826831	107.5813503	jawa
way	1497686765	pertamina	Pertamina	SPBU PERTAMINA	-6.1490198	106.6739757	sumatra
way	1498387338	pertamina	Pertamina	Pertamina	-6.8884525	107.5559552	jawa
way	1503991371	pertamina	Pertamina	Pertamina	-6.3604087	106.6189145	jawa
way	1504727479	pertamina	Pertamina	Pertamina	-6.9183352	107.6411605	jawa
way	1507069497	pertamina	Pertamina	Pertamina	-6.2501517	107.014672	jawa
way	1507805995	pertamina	Pertamina	Pertamina	-7.0390125	110.3304112	jawa
way	1515968511	pertamina	Pertamina	SPBU Pertamina 54.682.07 Besuk Klabang	-7.8181058	113.9655102	jawa
way	1528978250	shell	Shell	Shell	-7.2829342	112.650492	jawa
way	1530242914	shell	Shell	Shell	-7.3270159	112.7324132	jawa
way	1532865577	other	\\N	\\N	-6.5301469	110.7506319	jawa
way	1537410105	other	\\N	\\N	-6.8669945	112.356332	jawa
way	1538490915	pertamina	Pertamina	Pertamina	-7.7326147	112.0506528	jawa
way	1538490917	pertamina	Pertamina	Pertamina	-7.7537838	112.0324507	jawa
way	1538991053	pertamina	Pertamina	SPBU 54.671.08	-7.6368581	112.9011862	jawa
way	1539025158	pertamina	Pertamina	Pertamina Derwati	-6.9639313	107.6772452	jawa
node	330063880	pertamina	SPBU	SPBU	3.3222216	99.1613075	sumatra
node	456045476	pertamina	Pertamina	SPBU	0.5003504	101.4712846	sumatra
node	457837210	pertamina	Pertamina	Pertamina	0.6127166	101.4283773	sumatra
node	457837211	pertamina	Pertamina	Pertamina	0.6868751	101.4376946	sumatra
node	457837500	pertamina	Pertamina	Pertamina	0.5384361	101.4029521	sumatra
node	457837513	pertamina	Pertamina	Pertamina	0.573834	101.3985194	sumatra
node	462165004	pertamina	Pertamina	SPBU	0.5123009	101.3957259	sumatra
node	467530312	pertamina	Pertamina	Pertamina	0.4649989	101.5269557	sumatra
node	470790180	pertamina	Pertamina	SPBU Pertamina	0.5136399	101.4896786	sumatra
node	470790469	pertamina	Pertamina	Pertamina	0.4578049	101.5684447	sumatra
node	470793864	pertamina	Pertamina	Pertamina	0.6563014	101.8044902	sumatra
node	700645194	pertamina	SPBU	SPBU	1.057885	103.964912	sumatra
node	702182327	pertamina	Fuel Station (Spbu Tembesi)	Fuel Station (Spbu Tembesi)	1.04005	104.0006	sumatra
node	702183002	other	Fuel Temiang	Fuel Temiang	1.06224	103.96833	sumatra
node	702328175	pertamina	Pertamina	Pertamina Petrol Station	1.0501057	103.9537265	sumatra
node	702350219	pertamina	Pertamina	PERTAMINA Sukajadi	1.11395	104.02223	sumatra
node	702352162	pertamina	Pertamina	PERTAMINA Duriangkang	1.0971574	104.0365789	sumatra
node	702670734	pertamina	Pertamina	PERTAMINA Al-Azhar	1.1336261	104.0212083	sumatra
node	703814989	pertamina	Pertamina	PERTAMINA Harbour Bay	1.15402	104.0023799	sumatra
node	705081941	other	Fuel Station (Rumah Hitam) Sekupang	Fuel Station (Rumah Hitam) Sekupang	1.11009	103.94953	sumatra
node	707537290	other	Fuel Bensin Mekar Sari	Fuel Bensin Mekar Sari	1.1113353	103.9786589	sumatra
node	707606017	other	Fuel Bensin Tiban 3	Fuel Bensin Tiban 3	1.1093389	103.9659597	sumatra
node	718932258	other	Fuel Uib	Fuel Uib	1.1212706	104.005137	sumatra
node	736621162	pertamina	Pertamina	PERTAMINA Belakang BCA Jodoh	1.1532001	104.0056799	sumatra
node	736630440	pertamina	Pertamina	PERTAMINA Seraya	1.14794	104.01703	sumatra
node	760475100	pertamina	Pertamina	PERTAMINA KDA Batam Kota	1.10157	104.07634	sumatra
node	760485238	pertamina	SPBU Kapital Raya	SPBU Kapital Raya	1.1072505	104.0903634	sumatra
node	760491346	pertamina	Fuel Station(Spbu Kabil)	Fuel Station(Spbu Kabil)	1.08391	104.1176	sumatra
node	799261457	pertamina	Pertamina	SPBU Arengka	0.4536484	101.418877	sumatra
node	799261468	pertamina	Pertamina	Pertamina	0.4479448	101.4179509	sumatra
node	799261493	pertamina	Pertamina	SPBU	0.4327473	101.4627859	sumatra
node	799261509	pertamina	Pertamina	Pertamina	0.5000036	101.5075745	sumatra
node	820947020	other	\\N	\\N	2.2088187	98.9712352	sumatra
node	820947037	other	\\N	\\N	2.0823571	98.9451229	sumatra
node	820947048	other	\\N	\\N	2.0247652	98.9737364	sumatra
node	820947051	other	\\N	\\N	2.3340863	99.0696017	sumatra
node	820947052	other	\\N	\\N	2.2699897	99.0020918	sumatra
node	820983157	other	\\N	\\N	0.8514851	99.5627765	sumatra
node	820983186	other	\\N	\\N	1.4481323	99.3055362	sumatra
node	820983200	other	\\N	\\N	0.358909	100.0599292	sumatra
node	820983248	other	\\N	\\N	0.8096515	99.5705089	sumatra
node	820983258	other	\\N	\\N	0.8995204	99.5476874	sumatra
node	820986745	pertamina	Pertamina	Pertamina	-0.0436844	100.2108726	sumatra
node	820986764	other	\\N	\\N	0.1555193	100.1621866	sumatra
node	820986768	pertamina	Pertamina	Pertamina	-0.3110902	100.3724663	sumatra
node	856982312	pertamina	Pertamina	PERTAMINA PIH	1.1233128	104.0578027	sumatra
node	859634969	pertamina	Pertamina	Pertamina	0.5402543	101.4501376	sumatra
node	859646823	pertamina	Pertamina	Pertamina	0.5415543	101.456291	sumatra
node	885991631	pertamina	Pertamina	SPBU	0.4774685	101.4530329	sumatra
node	901205298	pertamina	Pertamina	PERTAMINA Duta Mas	1.10486	104.03931	sumatra
node	903271275	pertamina	Pertamina	PERTAMINA Sungai Panas	1.1390662	104.027029	sumatra
node	1280782699	pertamina	Pertamina	SPBU A. Yani	-2.1239227	106.1069701	sumatra
node	1368722084	pertamina	Pertamina	Pertamina	1.5623969	99.2865801	sumatra
node	1369108451	pertamina	Pertamina	SPBU Pertamina	0.8880756	100.2997906	sumatra
node	1369151775	pertamina	Pertamina	Pertamina	0.7348087	100.5040773	sumatra
node	1369156535	pertamina	Pertamina	Pertamina	0.7005755	100.5479479	sumatra
node	1369177805	pertamina	Pertamina	Pertamina	0.6098048	100.6411815	sumatra
node	1370923765	pertamina	Pertamina	Pertamina	2.6664728	98.9435962	sumatra
node	1668695492	pertamina	Pertamina	Pertamina	-5.4209338	105.2526844	sumatra
node	1848326928	pertamina	SPBU PENGKALA	SPBU PENGKALA	4.0007094	97.3412939	sumatra
node	1849888547	other	\\N	\\N	4.4737322	95.7927377	sumatra
node	1850975650	pertamina	PERTAMINA	SPBU RAKLUNUNG	3.9891733	97.3436974	sumatra
node	1879016496	pertamina	Pertamina	Pertamina	-5.4445134	105.269672	sumatra
node	1934657811	pertamina	SPBU	SPBU Simpang Kimbang	-0.4619693	100.5790755	sumatra
node	1980660370	pertamina	SPBU Cubadag	SPBU Cubadag	-0.4644197	100.5453075	sumatra
node	1989177390	pertamina	SPBU Parak Juar	SPBU parak juar	-0.4496297	100.5881656	sumatra
node	2055935651	pertamina	Pertamina	SPBU	0.4809752	101.4066908	sumatra
node	2055935657	pertamina	Pertamina	SPBU	0.4990032	101.3640125	sumatra
node	2105116003	pertamina	Pertamina	SPBU Pertamina	1.2834813	101.1795752	sumatra
node	2108523159	pertamina	Pertamina	SPBU Pertamina	1.2868527	101.1928347	sumatra
node	2170588590	other	\\N	\\N	3.5414472	98.7165755	sumatra
node	2171314463	other	\\N	\\N	2.6670436	98.9421772	sumatra
node	2323985963	pertamina	Pertamina	\\N	3.5953015	98.6774626	sumatra
node	2410482763	pertamina	Pertamina	Pertamina	-5.2059272	105.1751012	sumatra
node	2419456159	pertamina	Pertamina	Pertamina	-4.8904716	105.2152522	sumatra
node	2526523019	pertamina	Pertamina	\\N	-4.9268151	105.5273559	sumatra
node	2600946017	other	Kios Bensin	Kios Bensin	-4.5496734	105.1144086	sumatra
node	2613700851	pertamina	Pertamina	Pertamina	1.2486034	97.639911	sumatra
node	2631648804	pertamina	Pertamina	\\N	1.2568845	97.6392432	sumatra
node	2631648805	pertamina	Pertamina	\\N	1.2706201	97.6352399	sumatra
node	2668835562	pertamina	Pertamina	Pertamina	-5.0860919	105.3526861	sumatra
node	2813165758	pertamina	Pertamina	\\N	3.0918729	98.4922425	sumatra
node	2813171193	pertamina	Pertamina	Pertamina	3.1950916	98.508829	sumatra
node	2815460733	other	\\N	\\N	3.5311758	98.4163802	sumatra
node	2823824388	other	\\N	\\N	3.4948226	98.3005017	sumatra
node	2962728956	pertamina	Pertamina	SPBU COCO	1.6775899	101.4634123	sumatra
node	2978689949	pertamina	Pertamina	SPBU	1.6776715	101.4380872	sumatra
node	3001930669	pertamina	Pertamina	Pertamina	0.7418013	101.1893174	sumatra
node	3001944295	pertamina	Pertamina	Pertamina	0.310469	100.9258298	sumatra
node	3021624637	pertamina	Pertamina	SPBU	1.6009208	101.3914271	sumatra
node	3024494938	pertamina	Pertamina	Pertamina	5.1824091	97.1395456	sumatra
node	3026542018	pertamina	Pertamina	SPBU	1.6657921	101.4479595	sumatra
node	3051403483	pertamina	Pertamina	Pertamina	0.4150543	101.0225042	sumatra
node	3051409688	pertamina	Pertamina	Pertamina	0.3320873	100.9726667	sumatra
node	3051467663	pertamina	Pertamina	Pertamina	-0.2625154	100.6003142	sumatra
node	3068107713	pertamina	SPBU Air Sebakul	SPBU Air Sebakul	-3.8319892	102.3460138	sumatra
node	3111350010	pertamina	Pertamina	SPBU Balai Raja	1.2135075	101.2396083	sumatra
node	3144050026	other	\\N	\\N	2.4482921	99.1541095	sumatra
node	3158949331	pertamina	SPBU	SPBU	3.7298772	96.8255716	sumatra
node	3160809191	pertamina	SPBU JULI	SPBU JULI	5.1919106	96.7019246	sumatra
node	3168932887	pertamina	Pertamina	Pom Bensin	1.4888179	102.129108	sumatra
node	3169694228	pertamina	Pertamina	Pom Bensin	1.4782497	102.1313645	sumatra
node	3216854965	pertamina	Pertamina	Pom Bensin	1.4569773	102.1393422	sumatra
node	3235993314	pertamina	SPBU Thamrin	SPBU Thamrin	0.5157975	101.4579334	sumatra
node	3237890505	pertamina	Pertamina	Pertamina	0.9335659	102.0470161	sumatra
node	3242943497	pertamina	Pertamina	Pertamina	1.3749367	102.1469112	sumatra
node	3242999262	pertamina	Pertamina	Pertamina	1.1062398	102.1049764	sumatra
node	3261710770	pertamina	Pertamina	SPBU	1.5250528	102.2071613	sumatra
node	3261816232	other	SPMA	SPMA	1.2940427	102.4640436	sumatra
node	3262170349	pertamina	Pertamina	Pertamina	1.4981339	102.4744203	sumatra
node	3328120468	pertamina	SPBU	SPBU	3.7305217	96.8493811	sumatra
node	3352222769	other	\\N	\\N	3.5986505	98.6843669	sumatra
node	3352222781	pertamina	Pertamina	SPBU Coco 11.201.101	3.6022359	98.6734488	sumatra
node	3435347276	pertamina	SPBU Pertamina	SPBU Pertamina	1.14157	101.2797731	sumatra
node	3441376501	pertamina	Pertamina	Pertamina	2.9254257	99.7040789	sumatra
node	3478391316	pertamina	Pertamina	SPBU	0.8252322	102.0260585	sumatra
node	3546140269	pertamina	Pertamina	Pertamina	-4.9268412	105.2127562	sumatra
node	3584856293	other	\\N	\\N	3.6078806	98.4315028	sumatra
node	3638556931	pertamina	SPBU KKSP	SPBU KKSP	-0.9567537	100.4647751	sumatra
node	3685276831	pertamina	SPBU Pertamina	SPBU Pertamina	-5.2579934	105.1848613	sumatra
node	3881209320	pertamina	Pertamina	Pertamina	3.4556331	98.6014162	sumatra
node	3884339189	pertamina	SPBU	SPBU	1.1072565	104.0903782	sumatra
node	3908448642	pertamina	Pertamina	\\N	3.586059	98.6875698	sumatra
node	3911883976	pertamina	Pertamina	Singapore Station	3.5942519	98.6686924	sumatra
node	3912831335	pertamina	Pertamina	Pertamina	3.6878472	98.6620353	sumatra
node	3920228845	pertamina	Pertamina	SPBU Coco 11.201.107	3.5986747	98.6954896	sumatra
node	3920228846	pertamina	Pertamina	\\N	3.6248217	98.7074362	sumatra
node	3920228852	pertamina	Pertamina	\\N	3.6393119	98.6809592	sumatra
node	3920231560	pertamina	Pertamina	\\N	3.6423783	98.6761006	sumatra
node	3920231562	pertamina	Pertamina	\\N	3.6550867	98.6641648	sumatra
node	3950000541	pertamina	Pertamina	Pertamina	-5.3779742	105.2515797	sumatra
node	3952996326	pertamina	Pertamina	SPBU	-5.3979324	105.2041846	sumatra
node	3953398712	pertamina	Pertamina	Pom Kota Gajah	-4.9832409	105.3167432	sumatra
node	3982110757	other	\\N	\\N	3.020201	99.0846565	sumatra
node	3991394940	pertamina	SPBU 24.317.101 Tanjung Enim	SPBU 24.317.101 Tanjung Enim	-3.7306104	103.8013988	sumatra
node	4015807330	pertamina	Pertamina	Pertamina	0.491263	101.4550192	sumatra
node	4195087921	pertamina	Danau Raja	SPBU	-0.3685061	102.5223889	sumatra
node	4195090024	pertamina	Azki Aris	SPBU	-0.3701798	102.524127	sumatra
node	4201262687	pertamina	SPBU	SPBU	0.4909297	101.3949305	sumatra
node	4233015195	pertamina	SPBU 23.331.11	SPBU 23.331.11	-2.1016868	106.123387	sumatra
node	4264427752	other	Bensin	Bensin	5.8816561	95.2441845	sumatra
node	4264580247	pertamina	pertamina	SPBU Tapak Gajah	5.8993837	95.3222209	sumatra
node	4271458272	pertamina	Pertamina	Pertamina	3.592405	98.6698434	sumatra
node	4271458273	pertamina	Pertamina	Pertamina	3.5945545	98.6719463	sumatra
node	4278109369	pertamina	Pertamina	Pertamina	3.622242	98.6702813	sumatra
node	4295522091	pertamina	Pertamina	SPBU Batu Hitam	0.9106431	104.4460854	sumatra
node	4310427392	pertamina	SPBU 24.331.67	SPBU 24.331.67	-2.162408	106.1316794	sumatra
node	4310447789	other	Pom Bensin Asrama	Pom Bensin Asrama	3.5925712	98.6269415	sumatra
node	4316669292	pertamina	SPBU 24.332.133	SPBU 24.332.133	-1.8973124	106.1108684	sumatra
node	4329084902	pertamina	Pertamina	\\N	3.5976944	98.7202159	sumatra
node	4355006591	pertamina	SPBU 24.331.151	SPBU 24.331.151	-2.1339797	106.1479685	sumatra
node	4367300390	pertamina	SPBU 24.33168	SPBU 24.33168	-2.1381064	106.1197216	sumatra
node	4378527294	other	pertains	pertains	0.991585	104.6319114	sumatra
node	4378769631	pertamina	Pertamina	\\N	3.6082468	98.6513558	sumatra
node	4395480299	pertamina	SPBU Kota Tapaktuan	SPBU Kota Tapaktuan	3.2535278	97.174993	sumatra
node	4409030444	pertamina	SPBU 14.20111.26	SPBU 14.20111.26	3.5601052	98.6892123	sumatra
node	4409056693	pertamina	SPBU	SPBU	3.6148006	98.6643661	sumatra
node	4409057433	pertamina	SPBU	SPBU	3.489735	98.6832085	sumatra
node	4539697807	pertamina	Pertamina	Pertamina	3.5589288	98.6939798	sumatra
node	4542750379	pertamina	SPBU Kasman Lizar	SPBU Kasman Lizar	2.6285781	98.0197607	sumatra
node	4547857498	pertamina	SPBU	SPBU	3.5967836	98.7391736	sumatra
node	4559535473	pertamina	SPBU koto nan IV	SPBU koto nan IV	-0.2345107	100.6182923	sumatra
node	4560401710	pertamina	SPBU	SPBU	0.4691155	101.3937606	sumatra
node	4643110895	pertamina	Pertamina	Pertamina	1.3664829	99.283315	sumatra
node	4650433287	pertamina	Pertamina	Pertamina	1.3803219	99.2707822	sumatra
node	4650433475	pertamina	Pertamina	Pertamina	1.3891289	99.2744955	sumatra
node	4794772621	pertamina	Spbu Alue Awee	Spbu Alue Awee	5.1336454	97.1512894	sumatra
node	4802865521	pertamina	SPBU Tj Piayu	SPBU	1.0440619	104.0541177	sumatra
node	4826957659	pertamina	Pertamina	\\N	3.2797194	98.5535283	sumatra
node	4826957660	pertamina	Pertamina	\\N	2.945385	98.5203672	sumatra
node	4826957661	pertamina	Pertamina	\\N	3.020069	98.5393733	sumatra
node	4845895546	pertamina	Pertamina	\\N	3.9193004	98.3983344	sumatra
node	4845895548	pertamina	Pertamina	Pertamina	3.8796073	98.4215409	sumatra
node	4974487842	pertamina	Pertamina	Pertamina	5.8667259	95.3450303	sumatra
node	4992819584	akr	AKR	\\N	3.2031836	98.5396479	sumatra
node	5033366321	pertamina	SPBU	SPBU	5.4442979	95.7295169	sumatra
node	5033366323	other	lampaseh jaya no 14.231.482	lampaseh jaya no 14.231.482	5.5634873	95.323575	sumatra
node	5051514025	pertamina	SPBU	SPBU	-5.4321575	105.2536259	sumatra
node	5076419134	pertamina	Pertamina	Pertamina	2.6054001	98.7017231	sumatra
node	5076419136	akr	AKR	\\N	3.1947907	98.5420096	sumatra
node	5105421411	pertamina	Pertamina	\\N	3.0832015	98.5161826	sumatra
node	5162200621	other	SPPBE Aek Godang	SPPBE Aek Godang	1.3857608	99.4309799	sumatra
node	5179350734	pertamina	SPBU	SPBU	0.5169732	101.4321512	sumatra
node	5180907780	pertamina	SPBU	SPBU	0.5632862	101.4320142	sumatra
node	5180907781	pertamina	SPBU	SPBU	0.5674919	101.4298362	sumatra
node	5184506459	pertamina	Pertamina	SPBU	0.5168784	101.4110106	sumatra
node	5188010121	pertamina	SPBU	SPBU	0.5282443	101.4401536	sumatra
node	5188010125	pertamina	SPBU	SPBU	0.5035841	101.4277904	sumatra
node	5188010127	pertamina	SPBU	SPBU	0.5383888	101.4298362	sumatra
node	5238861421	pertamina	Pertamina	Pertamina	-3.7002989	103.2832388	sumatra
node	5238978734	pertamina	Pertamina	Pertamina	-3.669155	103.6988739	sumatra
node	5239001025	pertamina	Pertamina	Pertamina	-3.6092312	103.1007077	sumatra
node	5239001026	pertamina	Pertamina	Pertamina	-4.0789408	104.0821913	sumatra
node	5239002821	pertamina	Pertamina	Pertamina	-3.7860317	103.5557814	sumatra
node	5239040221	pertamina	Pertamina	SPBU batukuning	-4.1003261	104.1578583	sumatra
node	5296074099	pertamina	Pertamina	Pom Bensin	-5.4885833	104.593765	sumatra
node	5346205621	other	\\N	\\N	2.7386271	98.3690662	sumatra
node	5346205622	pertamina	Pertamina	Pertamina	3.1054853	98.4985474	sumatra
node	5352351657	pertamina	Pertamina	\\N	1.6007379	100.9772891	sumatra
node	5352351658	pertamina	Pertamina	\\N	1.6996918	100.677166	sumatra
node	5352351660	pertamina	Pertamina	\\N	1.6849086	100.4239118	sumatra
node	5352351668	pertamina	Pertamina	Pertamina	1.7870672	100.2017793	sumatra
node	5352351669	pertamina	Pertamina	\\N	1.8465475	100.1692039	sumatra
node	5352351670	pertamina	Pertamina	\\N	1.890582	100.1103133	sumatra
node	5352351671	other	\\N	\\N	1.9499703	100.0993189	sumatra
node	5352351672	pertamina	Pertamina	\\N	2.0583912	99.965474	sumatra
node	5352351673	pertamina	Pertamina	Pertamina	2.0935373	99.8363471	sumatra
node	5352351676	pertamina	Pertamina	\\N	2.4705892	99.643861	sumatra
node	5352351677	pertamina	Pertamina	Pertamina	2.9718309	99.6130156	sumatra
node	5352351678	pertamina	Pertamina	\\N	2.9746568	99.6301281	sumatra
node	5360889142	other	\\N	\\N	-3.8108866	102.2698503	sumatra
node	5380137034	other	\\N	\\N	1.1322074	104.0111899	sumatra
node	5592290663	pertamina	Pertamina	Pertamina	1.1577339	104.1247993	sumatra
node	5645142639	other	\\N	\\N	3.4240114	98.9046302	sumatra
node	5645157821	other	\\N	\\N	3.4945858	98.9034034	sumatra
node	5645206122	other	\\N	\\N	5.1158832	97.2025493	sumatra
node	5645206125	other	\\N	\\N	2.9971942	98.5432659	sumatra
node	5645224421	other	\\N	\\N	4.5968451	97.9146498	sumatra
node	5661217188	pertamina	Pertamina	Pertamina	0.5048218	101.4516289	sumatra
node	5663901321	pertamina	SPBU PERTAMINA 14.284.6111 Teratak Buluh	SPBU PERTAMINA 14.284.6111 Teratak Buluh	0.4091918	101.4346893	sumatra
node	5666025940	pertamina	pertamina jeulingke 14.239.405	pertamina jeulingke 14.239.405	5.5723111	95.3438974	sumatra
node	5757926621	pertamina	Pertamina	Pertamina	-0.2958476	100.3951244	sumatra
node	5761150822	pertamina	Pertamina	Pertamina	-0.2856405	100.4580589	sumatra
node	5767323153	pertamina	Pertamina	Pertamina	-0.9555896	100.3682176	sumatra
node	5767323753	pertamina	Pertamina	Pertamina	-1.0315364	100.4151975	sumatra
node	5772036553	pertamina	Pertamina	Pertamina	-0.944829	100.3706329	sumatra
node	5863186832	pertamina	Pertamina	SPBU	4.0159264	98.2876504	sumatra
node	5904066275	pertamina	Pertamina	Pertamina	-5.3829253	105.2589632	sumatra
node	5904515487	pertamina	SPBU Simpang Lubuk Batang	SPBU Simpang Lubuk Batang	-4.05549	104.2207316	sumatra
node	5905705992	pertamina	SPBU LAMPENEURUT 13.233.404	SPBU LAMPENEURUT 13.233.404	5.509888	95.3476271	sumatra
node	5905706000	pertamina	SPBU PAGAR AIR 14 233458	SPBU PAGAR AIR 14 233458	5.5269747	95.3624617	sumatra
node	5913412642	pertamina	SPBUN 18.232.004	SPBUN 18.232.004	5.5585105	95.2881324	sumatra
node	5920104775	pertamina	SPBU Lambhuk	SPBU Lambhuk	5.5539472	95.3372151	sumatra
node	5933664926	pertamina	SPBU 14.233463	SPBU 14.233463	5.5421843	95.3677211	sumatra
node	6051043930	other	\\N	\\N	1.1238838	104.1022044	sumatra
node	6302723685	pertamina	Pertamina	Pertamina	2.2694335	97.8168717	sumatra
node	6393411383	other	SPB Brimob	SPB Brimob	0.5182995	101.4375667	sumatra
node	6531427785	other	Petramina Gas Station	Petramina Gas Station	3.1743531	98.0495352	sumatra
node	6711367885	other	\\N	\\N	-0.9649602	100.3577316	sumatra
node	6727139476	pertamina	SPBU	SPBU	-4.6643426	105.2111505	sumatra
node	6851542541	other	pelabuhan	pelabuhan	3.0333733	105.7032583	sumatra
node	6866167439	pertamina	Pertamina	Pertamina	1.0455199	103.9614353	sumatra
node	6876142701	pertamina	Pertamina	PERTAMINA Bengkong	1.1588665	104.0324083	sumatra
node	6882325714	pertamina	Pertamina	Pertamina	1.0402071	103.9855979	sumatra
node	6960129397	pertamina	SPBU Yos Sudarso	SPBU Yos Sudarso	0.5379886	101.4362298	sumatra
node	7019691798	pertamina	Pertamina	SPBU 14.262.539	-0.2265334	100.6284061	sumatra
node	7023101934	pertamina	Pertamina	Pertamina	1.0012719	102.6993789	sumatra
node	7057317553	pertamina	Pertamina	Pertamina	5.3665768	95.6079734	sumatra
node	7057358761	pertamina	Pertamina	Pertamina	5.4447404	95.7295049	sumatra
node	7057361428	pertamina	Pertamina	Pertamina	5.3725706	95.8920888	sumatra
node	7083498380	pertamina	Pertamina	Pertamina	3.3190875	97.9501076	sumatra
node	7083528365	pertamina	Pertamina	Pertamina	3.4434522	97.8599455	sumatra
node	7187746047	other	\\N	\\N	0.4780181	101.3660379	sumatra
node	7657654311	other	Pom Bensin Perandonan	Pom Bensin Perandonan	-3.9708805	103.2135829	sumatra
node	7659509754	pertamina	Pom Spbu Pendopo	Pom Spbu Pendopo	-3.7897808	102.9700149	sumatra
node	7687475601	pertamina	Pom Spbu Pertamina Air Perikan	Pom Spbu Pertamina Air Perikan	-4.0295272	103.2358242	sumatra
node	7949579881	pertamina	Pertamina	\\N	2.9190197	98.6304501	sumatra
node	7973241725	pertamina	SPBU	SPBU	-5.2852918	104.9839776	sumatra
node	7973242297	pertamina	SPBU Baradatu	SPBU Baradatu	-4.7337144	104.5428452	sumatra
node	7973242315	pertamina	SPBU	SPBU	-5.0957016	105.19041	sumatra
node	7973242317	pertamina	SPBU	SPBU	-5.2417839	104.9699065	sumatra
node	7973242392	pertamina	SPBU Lampung Tengah	SPBU Lampung Tengah	-5.0124277	105.2070208	sumatra
node	8026875987	pertamina	Pertamina	\\N	3.0892491	98.4703088	sumatra
node	8045646573	pertamina	Pertamina	Pertamina	0.5246104	101.4751557	sumatra
node	8379956789	pertamina	Pertamina	SPBU Pertamina	1.26311	101.1472761	sumatra
node	8570735008	pertamina	Pertamina	Pertamina	1.1552488	104.0205083	sumatra
node	8582603064	other	Pump bensin dan indomaret muara enim	Pump bensin dan indomaret muara enim	-3.631576	103.7754466	sumatra
node	8828890334	pertamina	Pertamina	Pertamina	0.8629254	104.6059452	sumatra
node	8852183830	pertamina	Pertashop	Pertashop	-4.5416451	105.0903168	sumatra
node	8925258782	pertamina	Pertamina	Pertamina	-3.4438543	102.5121615	sumatra
node	9355074628	pertamina	Pertamina	PERTAMINA REGATA	1.1330585	104.042548	sumatra
node	9355230167	pertamina	Pertamina	Pertamina	1.1341777	104.0175385	sumatra
node	9355916707	pertamina	Pertamina	PERTAMINA Marchelia	1.122413	104.0326639	sumatra
node	9355932919	pertamina	Pertamina	PERTAMINA Aston	1.1423407	104.0205933	sumatra
node	9357287710	other	PGN Gas Station	PGN Gas Station	1.1280088	104.0583483	sumatra
node	9479323217	pertamina	Pom Pertamina	Pom Pertamina	-5.3887465	105.2880814	sumatra
node	9580886112	pertamina	Pertamina	Pertamina	3.6945515	98.6730823	sumatra
node	9587107284	pertamina	Pertashop	\\N	-1.442199	101.0179332	sumatra
node	9587107289	pertamina	Pertashop	\\N	-1.5441384	101.114181	sumatra
node	9587107290	pertamina	Pertashop	\\N	-1.5125699	101.0842264	sumatra
node	9663624918	pertamina	Pertamina	Pertamina	-0.242757	102.2481271	sumatra
node	9663669745	pertamina	Pertamina	Pertamina	0.6761777	99.6984127	sumatra
node	9710427404	other	\\N	\\N	4.4853384	97.9726946	sumatra
node	9717063879	pertamina	Pertamina	Pertamina	3.5677076	98.9525225	sumatra
node	9726425168	pertamina	Pertamina	Pertashop 2P.371.238	-2.1690214	101.5971799	sumatra
node	9728705132	pertamina	SPBU Pertamina	SPBU Pertamina	-5.0237566	103.7415933	sumatra
node	9730050917	pertamina	Pertamina	Pertamina	3.5379092	98.6892263	sumatra
node	9730050918	pertamina	Pertamina	Pertamina	3.5400787	98.6541996	sumatra
node	9763960710	pertamina	SPBU Batu 7	SPBU Batu 7	0.9162404	104.4815397	sumatra
node	9765678098	pertamina	SPBU Pertamina	SPBU Batu 8 Bawah	0.9244987	104.5040761	sumatra
node	9768531569	pertamina	SPBU Imam Bonjol	SPBU Imam Bonjol	-5.3939257	105.2322049	sumatra
node	9771280581	pertamina	SPBU Antasari	SPBU Antasari	-5.4031745	105.2817668	sumatra
node	9774244549	pertamina	SPBU Yos Sudarso	SPBU Yos Sudarso	-5.4492334	105.3042491	sumatra
node	9774327455	pertamina	SPBU Bumi Agung	SPBU Bumi Agung	-5.1736174	105.1820302	sumatra
node	9776297111	pertamina	SPBU Palapa	SPBU Palapa	-5.4216896	105.2469393	sumatra
node	9776301427	pertamina	SPBU Cut Nyak Dien	SPBU Cut Nyak Dien	-5.4141971	105.2470045	sumatra
node	9776323953	other	Pom Bensin Mini	Pom Bensin Mini	-5.3980155	105.2514916	sumatra
node	9776485938	pertamina	SPBU Gunung Sugih	SPBU Gunung Sugih	-4.9608077	105.2166563	sumatra
node	9776939453	pertamina	SPBU Kurungan Nyawa	SPBU Kurungan Nyawa	-5.3771992	105.1955799	sumatra
node	9776976852	pertamina	SPBUTaman Sari	SPBUTaman Sari	-5.3654719	105.1476329	sumatra
node	9777449379	pertamina	SPBU Tugu Pengantin	SPBU Tugu Pengantin	-5.3796243	105.0944259	sumatra
node	9778473088	pertamina	SPBU Gading Rejo	SPBU Gading Rejo	-5.3744651	105.0687818	sumatra
node	9778519557	pertamina	SPBU REST AREA Gadingrejo	SPBU REST AREA Gadingrejo	-5.3808089	105.0244308	sumatra
node	9778557551	other	Ale Pom Bensin	Ale Pom Bensin	-5.3635453	105.0039836	sumatra
node	9778561882	other	Masjid Al Syadar	Masjid Al Syadar	-5.3735689	105.0648439	sumatra
node	9778566170	pertamina	SPBU Tambak Rejo	SPBU Tambak Rejo	-5.3780438	105.0140132	sumatra
node	9778661750	pertamina	SPBU Fajar Resuk	SPBU Fajar Resuk	-5.3563356	104.9642781	sumatra
node	9778824803	pertamina	SPBU Kali Balangan	SPBU Kali Balangan	-4.8760842	104.9690584	sumatra
node	9780602970	pertamina	SPBU Pagelaran Kota	SPBU Pagelaran Kota	-5.3710775	104.8837586	sumatra
node	9780652392	pertamina	SPBU Tanjung Heran	SPBU Tanjung Heran	-5.3547327	104.7966231	sumatra
node	9780868747	pertamina	SPBU Gunung Tiga	SPBU Gunung Tiga	-5.0563112	105.4517082	sumatra
node	9784794948	pertamina	SPBU Pertamina Sudirman Liwa	SPBU Pertamina Sudirman Liwa	-5.0380531	104.0853199	sumatra
node	9785929251	pertamina	SPBU Ki Maja	SPBU Ki Maja	-5.3863312	105.2698593	sumatra
node	9785969649	pertamina	SPBU Sultan Agung	SPBU Sultan Agung	-5.3814502	105.2697401	sumatra
node	9786469801	pertamina	SPBU RA Basyid	SPBU RA Basyid	-5.3522152	105.2635458	sumatra
node	9787131635	pertamina	SPBU Karang Anyar	SPBU Karang Anyar	-5.3083765	105.2870573	sumatra
node	9787177731	pertamina	SPBU Simpang	SPBU Simpang	-5.2340764	105.3048619	sumatra
node	9787194245	pertamina	SPBU Kibang	SPBU Kibang	-5.1917413	105.3174352	sumatra
node	9788597891	pertamina	SPBU Tirtayasa	SPBU Tirtayasa	-5.4103528	105.319427	sumatra
node	9788692543	pertamina	SPBU Kali Asin	SPBU Kali Asin	-5.416837	105.3431352	sumatra
node	9789376438	pertamina	SPBU Serdang	SPBU Serdang	-5.4195155	105.4022256	sumatra
node	9789387379	pertamina	SPBU Kotabumi	SPBU Kotabumi	-4.8354239	104.8977832	sumatra
node	9789442226	pertamina	pertamina	SPBU Kotabumi	-4.8496314	104.9125624	sumatra
node	9789688281	pertamina	SPBU Terbanggi Besar	SPBU Terbanggi Besar	-4.8305173	105.225778	sumatra
node	9789802599	pertamina	SPBU	SPBU	-4.7570806	105.2128901	sumatra
node	9791514825	pertamina	SPBU Simpang Manna	SPBU Simpang Manna	-4.0338403	103.2640984	sumatra
node	9791603894	pertamina	SPBU Shell	SPBU Shell	3.5682277	98.9711188	sumatra
node	9791603907	pertamina	SPBU Pertamina	SPBU Pertamina	3.5690375	98.966791	sumatra
node	9791638099	pertamina	SPBU Pertamina	SPBU Pertamina	3.5667996	98.9992323	sumatra
node	9792123382	pertamina	SPBU Pertamina	SPBU Pertamina	3.5517734	99.0312633	sumatra
node	9793168653	pertamina	Pertamina Tebing Tinggi	Pertamina Tebing Tinggi	-3.5861455	103.0831067	sumatra
node	9794057209	pertamina	SPBU Pramuka	SPBU Pramuka	-5.3813426	105.2210747	sumatra
node	9794159814	pertamina	SPBU Rajabasa1	SPBU Rajabasa1	-5.3678694	105.2333941	sumatra
node	9794160659	pertamina	SPBU Rajabasa 2	SPBU Rajabasa 2	-5.3674838	105.233891	sumatra
node	9794427264	pertamina	SPBU AKR	SPBU AKR	-5.3640261	105.2758794	sumatra
node	9794432702	pertamina	SPBU Codo	SPBU Codo	-5.3632551	105.27803	sumatra
node	9794712653	pertamina	SPBU Way KAndis	SPBU Way KAndis	-5.3590246	105.2852565	sumatra
node	9794794529	pertamina	SPBU Jati Mulyo	SPBU Jati Mulyo	-5.3419425	105.294787	sumatra
node	9795414889	pertamina	SPBU Garuntang	SPBU Garuntang	-5.4355786	105.2846282	sumatra
node	9796116864	pertamina	SPBU Martadinata	SPBU Martadinata	-5.4647544	105.2444359	sumatra
node	9796139579	pertamina	Pertashop	Pertashop	-5.4521044	105.2550781	sumatra
node	9797510492	pertamina	SPBU Lintik	SPBU Lintik	-5.2296134	103.9244991	sumatra
node	9800497935	pertamina	SPBU Pertamina	SPBU Pertamina	3.5945169	98.6166045	sumatra
node	9801931514	pertamina	Pertamina	Pertamina	2.5120408	99.636805	sumatra
node	9808924194	pertamina	SPBU SUKABERENANG	SPBU SUKABERENANG	0.9144739	104.4529965	sumatra
node	9836075185	pertamina	Pertamina	\\N	0.9055713	104.488007	sumatra
node	9896556157	pertamina	SPBU Pattimura	SPBU Pattimura	-5.4444715	105.2663211	sumatra
node	9933038047	pertamina	AKR	SPBU AKR	-5.6477698	105.5477359	sumatra
node	9943973464	pertamina	Pertashop	Pertashop	-4.5816341	105.087514	sumatra
node	10091624007	other	\\N	\\N	-2.9442758	104.7585048	sumatra
node	10598442171	pertamina	Pertashop	Pertashop	-4.5134271	105.0908417	sumatra
node	10648564262	other	\\N	\\N	-0.3220252	100.5268996	sumatra
node	10648564263	other	\\N	\\N	-0.3644797	100.5528712	sumatra
node	10648564264	other	\\N	\\N	-0.3959582	100.5599653	sumatra
node	10701166696	other	\\N	\\N	2.6701085	98.8538998	sumatra
node	10704076934	pertamina	Pertamina	Pertamina	2.6917994	98.8198775	sumatra
node	10720845195	pertamina	Pertamina	Pertamina	2.9705417	99.0671096	sumatra
node	10991721289	pertamina	SPBU 14 202 102	SPBU 14 202 102	3.5590457	98.6938627	sumatra
node	10991751869	pertamina	SPBU 14.201.1126	SPBU 14.201.1126	3.5684386	98.6869178	sumatra
node	11104858476	pertamina	SPBU Pertamina	SPBU Pertamina	-5.0456932	105.2757242	sumatra
node	11581307545	pertamina	Pertamina	Pertamina	0.4638023	101.4063811	sumatra
node	11964280991	other	\\N	\\N	-2.0293337	99.5883973	sumatra
node	11999369430	pertamina	Pertamina	Pertamina Arengka	0.4699007	101.4180274	sumatra
node	12084269802	pertamina	SPBU Way Jepara	SPBU Way Jepara	-5.1740137	105.7028504	sumatra
node	12269529287	pertamina	Pertamina	Pertamina	-3.0305563	104.7207369	sumatra
node	12269529392	pertamina	Pertamina	Pertamina	-3.0437758	104.7315448	sumatra
node	12406101637	pertamina	Pertamina	Pertamina	-2.4744493	103.0242801	sumatra
node	12408780164	other	\\N	\\N	1.0528949	103.9305698	sumatra
node	12418092102	pertamina	Pertamina	Pertamina	3.6061688	98.4708213	sumatra
node	12450789691	other	\\N	\\N	3.6023173	98.5390838	sumatra
node	12450886431	pertamina	Pertamina	Pertamina	3.538237	98.6905438	sumatra
node	12452453679	pertamina	Pertamina	Pertamina	3.553147	98.8484033	sumatra
node	12452453928	pertamina	Pertamina	Pertamina	3.5473682	98.8159172	sumatra
node	12452684134	akr	AKR	AKR	2.7070734	99.6236661	sumatra
node	12452698631	pertamina	Pertamina	Pertamina	2.839926	99.6436511	sumatra
node	12452773538	pertamina	Pertamina	Pertamina	2.3133817	99.7346448	sumatra
node	12452779018	pertamina	Pertamina	Pertamina	2.2580284	99.7767948	sumatra
node	12498455757	pertamina	Pertamina	SPBU Srengsem	-5.4932649	105.3263333	sumatra
node	12638694583	pertamina	Pertamina	Pertamina	1.0331278	104.6562909	sumatra
node	12645421501	pertamina	Pertamina	Pertamina	1.0384357	104.3513927	sumatra
node	12811274812	pertamina	Pertamina	Pertamina	-1.4891964	102.0990575	sumatra
node	12880312001	pertamina	SPBU perbatasan Mpa-Lampung	SPBU perbatasan Mpa-Lampung	-4.376231	104.3679675	sumatra
node	12880422401	pertamina	SPBU COT	SPBU COT	-4.3110206	104.332423	sumatra
node	12883509201	pertamina	SPBU Bakung	SPBU Bakung	-4.1134727	104.1740272	sumatra
node	12898466202	pertamina	SPBU	SPBU	-2.9628063	104.7413924	sumatra
node	12925568001	pertamina	SPBU Lembak	SPBU Lembak	-3.3568212	104.3255956	sumatra
node	12979154683	pertamina	Pertamina	Pertamina	-4.2923821	105.3018335	sumatra
node	13021391209	pertamina	Pertamina	Pertashop - Tanjong Keumala	5.2197715	96.9590755	sumatra
node	13033605901	pertamina	SPBU KOTABUMI	SPBU KOTABUMI	-4.8406483	104.8717205	sumatra
node	13207936601	pertamina	SPBU simpang air paoh	SPBU simpang air paoh	-4.1221219	104.1823867	sumatra
node	13310731088	pertamina	SPBU Air Ruay Kec.Pemali	SPBU Air Ruay Kec.Pemali	-1.8769672	106.0832404	sumatra
node	13350151274	akr	AKR	\\N	4.0775462	98.1170593	sumatra
node	13377567600	other	\\N	\\N	4.254806	98.0616627	sumatra
node	13377596252	other	\\N	\\N	4.254806	98.0616627	sumatra
node	13380506612	other	\\N	\\N	4.3068569	98.0504542	sumatra
node	13380515599	other	\\N	\\N	5.1251545	97.4442443	sumatra
node	13386871301	pertamina	SPBU Coco Aceh 11.231.401	SPBU Coco Aceh 11.231.401	5.5247463	95.3025899	sumatra
node	13410044233	other	\\N	\\N	3.5937444	98.4909285	sumatra
node	13429921943	pertamina	Pertamina	Pertamina	3.5024898	97.8067072	sumatra
node	13439044410	pertamina	Pertamina	Pertamina	0.1218569	100.1726568	sumatra
node	13449464589	pertamina	Pertamina	Pertamina	1.7920314	99.0984536	sumatra
node	13462980588	pertamina	Pertamina	Pertamina	1.0312348	99.4838041	sumatra
node	13503216943	pertamina	Pertamina	Pertamina	-0.8064454	100.6547787	sumatra
node	13503239145	pertamina	Pertamina	Pertamina	-0.7875713	100.6456124	sumatra
node	13515781035	pertamina	Pertamina	Pertamina	-0.2471582	100.6111543	sumatra
node	13608841351	other	\\N	\\N	-0.2109109	100.6157807	sumatra
node	13613497640	pertamina	Pertamina	Pertamina	-0.1303708	100.696277	sumatra
node	13694882375	pertamina	Pertamina	SPBU Pertamina Meredan 16.287.058	0.6166352	101.6465528	sumatra
node	13921549243	pertamina	SPBU Sumani	SPBU Sumani	-0.720525	100.59946	sumatra
node	13994418001	pertamina	Pertamina	Pertamina	-2.0989174	106.1116711	sumatra
way	122617363	pertamina	Independent	Sarana Pengisian Bahan Bakar Nelayan	-2.0867942	106.1540427	sumatra
way	123621273	pertamina	Pertamina	SPBU Sp. Kepoh	-2.9928577	106.4783766	sumatra
way	125199368	pertamina	Pertamina	SPBU Berok	-2.4872387	106.406561	sumatra
way	126625555	pertamina	Depot Pertamina Aviation	Depot Pertamina Aviation	-2.160509	106.1365565	sumatra
way	129380668	other	\\N	\\N	-2.1236875	106.106743	sumatra
way	146894104	pertamina	Pertamina	SPBU 14.251.509	-0.9367454	100.3543552	sumatra
way	152108886	pertamina	Pertamina	SPBU Khatib Sulaeman	-0.9145513	100.3587572	sumatra
way	157806653	pertamina	SPBU Mato Aia Padang	SPBU Mato Aia Padang	-0.9789059	100.3811506	sumatra
way	161454107	other	\\N	\\N	-0.9306806	100.3981198	sumatra
way	166401443	pertamina	Pertamina	SPBU Jati	-0.9412777	100.3658609	sumatra
way	167865787	pertamina	Pertamina	SPBU Marapalam	-0.950583	100.3864069	sumatra
way	174265244	pertamina	SPBU	SPBU	3.5991107	98.5624811	sumatra
way	181983612	pertamina	Pertamina	SPBU 14.251.583	-0.9099628	100.3543012	sumatra
way	185261081	pertamina	Pertamina	Pertamina	-4.6216704	105.1077556	sumatra
way	236027700	pertamina	Pertamina	SPBU Kampung Bali	-3.7842243	102.2657196	sumatra
way	236957366	pertamina	Pertamina	SPBU Rawa Makmur	-3.7659913	102.2692974	sumatra
way	248410101	pertamina	Pertamina	SPBU Babahrot	3.8836207	96.7120044	sumatra
way	267157882	pertamina	SPBU	SPBU	3.5662466	98.6921416	sumatra
way	275867478	pertamina	Pertamina	SPBU Simpang Sawmill	-1.3995078	102.0302473	sumatra
way	287495701	pertamina	Pertamina	SPBU Muaro Sijunjung	-0.6597936	100.9236169	sumatra
way	288605246	other	\\N	\\N	3.5750114	98.6886104	sumatra
way	291695090	pertamina	Pertamina	Pertamina	-2.9414251	104.7269963	sumatra
way	292809863	pertamina	Pertamina	Pertamina	3.6363379	98.6667656	sumatra
way	293146455	other	\\N	\\N	2.1195636	100.8214896	sumatra
way	294528836	pertamina	SPBU	SPBU	1.2808472	97.6276394	sumatra
way	300883333	pertamina	Pertamina	Pertamina	5.2164427	97.0598235	sumatra
way	301302898	pertamina	Pertamina	\\N	5.2102909	97.1009671	sumatra
way	301748113	pertamina	Pertamina	SPBU Muara Labuh	-1.4768508	101.0479084	sumatra
way	301796525	pertamina	Pertamina	Pertamina	5.1791731	97.146564	sumatra
way	302681838	pertamina	Pertamina	Pertamina	5.2041427	97.1318527	sumatra
way	302852345	pertamina	Pertamina	SPBU Pagar Dewa	-3.8428032	102.3216506	sumatra
way	302852348	pertamina	Pertamina	SPBU Km. 8	-3.8270375	102.3027436	sumatra
way	302900655	pertamina	Pertamina	SPBU Km. 6	-3.8189917	102.2951585	sumatra
way	302900660	pertamina	Pertamina	SPBU Bumi Ayu	-3.8527038	102.3140572	sumatra
way	303003594	pertamina	Pertamina	SPBU 24.382.05	-3.8086997	102.2929712	sumatra
way	305083025	pertamina	Pertamina	Pertamina	5.5289284	95.2956204	sumatra
way	307967191	pertamina	Pertamina	SPBU Kandang	-3.8701414	102.3121367	sumatra
way	310756824	pertamina	Pertamina	SPBU Sangir, Solok Selatan	-1.56494	101.254949	sumatra
way	310920067	pertamina	Pertamina	SPBU Mangkunegara	-2.9397799	104.7675336	sumatra
way	310920348	pertamina	Pertamina	SPBU 24.301.120	-2.9433515	104.7840853	sumatra
way	311496931	pertamina	Pertamina	SPBU 24-301-12	-2.9562777	104.7879731	sumatra
way	312704863	pertamina	Pertamina	SPBU 24.301.16	-2.9518575	104.7585785	sumatra
way	316672432	pertamina	Pertamina	Pertamina	5.1699005	97.1340371	sumatra
way	317491756	pertamina	Pertamina	SPBU 24.302.20	-3.0116496	104.7579936	sumatra
way	317509558	pertamina	Pertamina	Pertamina	0.4647068	101.5267463	sumatra
way	325937386	pertamina	Pertamina	\\N	3.5932495	98.7117807	sumatra
way	326190999	pertamina	SPBU	SPBU	3.7304831	96.8494017	sumatra
way	326373051	pertamina	Pertamina	Pertamina	3.5976193	98.7087697	sumatra
way	326765725	pertamina	Pertamina	Pertamina	3.6289635	98.707234	sumatra
way	327366256	pertamina	Pertamina	SPBU 14.282.6117	0.4633569	101.4063792	sumatra
way	327366398	pertamina	Pertamina	SPBU Kubang Raya	0.4164303	101.3722755	sumatra
way	331362687	other	\\N	\\N	3.6031233	98.6801818	sumatra
way	331362688	other	\\N	\\N	3.6032697	98.6802189	sumatra
way	336868037	other	\\N	\\N	1.2548052	101.2137988	sumatra
way	337026911	pertamina	Pertamina	\\N	3.63028	98.6787415	sumatra
way	337026912	pertamina	Pertamina	Pertamina	3.6294743	98.6805649	sumatra
way	338420458	pertamina	Pertamina	SPBU Pertaminan Belinyu	-1.647969	105.7881773	sumatra
way	338421200	pertamina	Pertamina	SPBU Pertamina	-1.63767	105.7713553	sumatra
way	339191139	pertamina	Pertamina	\\N	3.5781726	98.6770289	sumatra
way	345580870	other	\\N	\\N	5.200408	96.7692517	sumatra
way	348833338	pertamina	Pertamina	\\N	-1.4984734	102.1049634	sumatra
way	357792722	pertamina	Pertamina	\\N	3.6087242	98.4991223	sumatra
way	358059795	pertamina	Pertamina	SPBU 24.372.21	-1.4512213	102.1230556	sumatra
way	362255592	pertamina	Pertamina	SPBU Sukajaya	-1.880515	103.5935955	sumatra
way	362256010	pertamina	Pertamina	SPBU Mekar Jaya	-1.9448226	103.6489617	sumatra
way	362256575	pertamina	Pertamina	SPBU Tempino	-1.7785653	103.5184857	sumatra
way	362257769	pertamina	Pertamina	SPBU 24.306.30	-3.3861486	104.8277726	sumatra
way	362258593	pertamina	Pertamina	SPBU Sungai Buluh	-1.7081413	103.3146632	sumatra
way	362258867	pertamina	Pertamina	SPBU Sungai Puar	-1.6432834	102.9295611	sumatra
way	362260298	pertamina	Pertamina	SPBU 24.307.157	-2.4515994	103.9520268	sumatra
way	371840458	pertamina	Pertamina	SPBU Karang Dalo	-4.0542016	103.293799	sumatra
way	373164926	pertamina	Pertamina	SPBU Tanah Patah	-3.8024753	102.2754696	sumatra
way	374547068	pertamina	Pertamina	SPBU 24.307.132	-2.9280667	104.5555973	sumatra
way	374657282	pertamina	Pertamina	SPBU 24.301.108	-2.9128448	104.6848614	sumatra
way	374677419	pertamina	Pertamina	Pomp Bensin	-2.8634733	104.365451	sumatra
way	374787877	pertamina	Pertamina	SPBU 24.307.83	-2.8976921	104.4071823	sumatra
way	405924503	pertamina	SPBU H Zamhur NO.1.231.484	SPBU H Zamhur NO.1.231.484	5.5350786	95.3306703	sumatra
way	419708740	pertamina	Pertamina	SPBU 14.284.623	0.4648679	101.3733318	sumatra
way	419997378	pertamina	Pertamina	SPBU Simpang 4 Sidomulyo	-0.3450799	102.3284334	sumatra
way	421499718	pertamina	Pertamina	SPBU Sukakarya Pekan Baru	0.4429436	101.3857648	sumatra
way	430134824	pertamina	Pertamina	SPBU 24.333.76	-2.0565017	105.1719205	sumatra
way	431490214	pertamina	Pertamina	SPBU Pertamina	-1.7372153	105.8612393	sumatra
way	431492980	pertamina	Pertamina	SPBU 24.333.77	-1.8965547	105.7058253	sumatra
way	435324539	pertamina	Pertamina	\\N	3.5982677	98.6918107	sumatra
way	438226579	pertamina	Pertamina	SPBU Kembang Sri	-3.7911438	102.3598329	sumatra
way	439335803	other	SPUB Biha	SPUB Biha	-5.3623726	104.0424378	sumatra
way	439387644	pertamina	Pertamina	\\N	3.6246959	98.6597154	sumatra
way	441608041	pertamina	Pertamina	SPBU Gadung	-2.9655535	106.4728908	sumatra
way	441936305	pertamina	Pertamina	SPBU 24.345.110	-5.0168148	104.1410628	sumatra
way	442009214	pertamina	Pertamina	SPBU Tegineneng	-5.1860048	105.1783725	sumatra
way	442024036	pertamina	Pertamina	SPBU Tarahan	-5.5510071	105.3689156	sumatra
way	442927698	pertamina	Pertamina	SPBU Parit 9	-3.007721	106.4998025	sumatra
way	443121609	pertamina	Pertamina	SPBU Sendawar	-4.2384648	102.7782873	sumatra
way	443738103	pertamina	Pertamina	SPBU Maje	-4.8586687	103.4513552	sumatra
way	444224570	pertamina	Pertamina	SPBU Bintuhan	-4.7957226	103.3567467	sumatra
way	444865410	pertamina	Pertamina	SPBU 24.383.15	-3.6675318	102.2345049	sumatra
way	445856336	pertamina	Pertamina	SPBU 24.351.33	-5.3964971	105.2890972	sumatra
way	446752022	pertamina	Pertamina	SPBU Kota Agung	-5.4949778	104.6274809	sumatra
way	449889569	pertamina	Pertamina	SPBU Temayang	-2.978695	106.444844	sumatra
way	453425801	pertamina	Pertamina	SPBU Pal 9	-1.5369528	102.0995942	sumatra
way	459623423	pertamina	Pertamina	SPBU 14.282.636	0.4699147	101.4179326	sumatra
way	461332158	pertamina	Pertamina	SPBU 24.351.113	-5.3910845	105.2786148	sumatra
way	466295185	pertamina	Pertamina	SPBU Pertamina Arifin Ahmad	0.4780484	101.4308653	sumatra
way	485832446	other	Pom Bensin	Pom Bensin	1.4569673	102.1392998	sumatra
way	485892441	pertamina	Pertamina	SPBU 14.282.650	0.4335289	101.4438459	sumatra
way	488871157	pertamina	Pertamina	\\N	3.715575	98.6625688	sumatra
way	492742967	pertamina	Pertamina	SPBU-14231462	5.5182873	95.3154863	sumatra
way	505277866	pertamina	Pertamina	Pertamina 14.204.117	3.7382852	98.6763763	sumatra
way	514768418	pertamina	Pertamina	SPBU Mangkol	-2.1484622	106.0952717	sumatra
way	528461321	pertamina	Pertamina	SPBU 24.314.44	-3.7630558	103.5016849	sumatra
way	533084233	pertamina	Pertamina	SPBU Muara Bulian	-1.7213324	103.2466707	sumatra
way	539148985	pertamina	Pertamina	SPBU Semaran	-2.1135058	102.861693	sumatra
way	539149898	pertamina	Pertamina	SPBU Bernai	-2.3027376	102.6950188	sumatra
way	542040752	pertamina	Pertamina	Pom Bensin Bandar Buat	-0.9492509	100.4338251	sumatra
way	544245026	pertamina	SPBU mini	SPBU mini	1.6567247	101.4526616	sumatra
way	544980750	pertamina	Pertamina	SPBU Rantau Puri	-1.6689932	103.325746	sumatra
way	545731248	pertamina	SPBU Bakongan	SPBU Bakongan	2.9156793	97.5019931	sumatra
way	545737609	pertamina	SPBU Oyon	SPBU Oyon	2.6479127	98.0028559	sumatra
way	546606438	pertamina	Pertamina	Pertamina	-0.519809	102.9243179	sumatra
way	546686636	pertamina	Pertamina	Pertamina	0.4567153	101.3547743	sumatra
way	546982984	pertamina	Pertamina	SPBU Bangkinang	0.3459886	101.0274753	sumatra
way	547620553	pertamina	Pertamina	SPBU Durian Luncuk	-1.8754461	103.0034234	sumatra
way	548455762	other	\\N	\\N	3.5731942	98.7030121	sumatra
way	549165032	pertamina	Pertamina	SPBU Tais	-4.0735845	102.5776546	sumatra
way	549716930	other	\\N	\\N	3.5696876	98.6778058	sumatra
way	549739035	other	\\N	\\N	3.5320834	98.6432885	sumatra
way	549741037	pertamina	SPBU Pertamina	SPBU Pertamina	3.5559807	98.6260538	sumatra
way	550041105	pertamina	SPBU	SPBU	3.5847481	98.6365099	sumatra
way	550936840	other	\\N	\\N	3.5787799	98.626877	sumatra
way	556522723	other	\\N	\\N	3.5746239	98.6636774	sumatra
way	556522724	other	\\N	\\N	3.5741879	98.6646566	sumatra
way	556829875	pertamina	SPBU Pertamina	SPBU Pertamina	3.5248825	98.6295152	sumatra
way	556850269	pertamina	SPBU 14.201.1144	SPBU 14.201.1144	3.5018385	98.6126401	sumatra
way	556858048	pertamina	SPBU Pertamina	SPBU Pertamina	3.541303	98.6235264	sumatra
way	556936836	other	pom bensin polisi lamteumen barat asrama polisi	pom bensin polisi lamteumen barat asrama polisi	5.530399	95.2954762	sumatra
way	558717811	pertamina	Pertamina	SPBU Korem	-3.4875215	102.5203471	sumatra
way	559572301	pertamina	SPBU 14.201.1152	SPBU 14.201.1152	3.5378409	98.6632692	sumatra
way	560172455	other	\\N	\\N	3.5659365	98.6411904	sumatra
way	560405693	pertamina	SPBU Ope Udan	SPBU Ope Udan	3.4876681	98.5990624	sumatra
way	560766656	pertamina	Pertamina	SPBU Muara Tebo	-1.4677883	102.4392593	sumatra
way	560766657	pertamina	Pertamina	\\N	-1.4617003	102.4612507	sumatra
way	560766666	pertamina	Pertamina	SPBU Sungai Bengkal	-1.5350631	102.6985294	sumatra
way	561629507	pertamina	SPBU	SPBU	3.5846513	98.6491496	sumatra
way	561629509	other	\\N	\\N	3.5776155	98.6434907	sumatra
way	569908311	pertamina	Pertamina	SPBU 24.275.506	-0.7642192	101.0357088	sumatra
way	572282838	pertamina	Pertamina	SPBU Silaut	-2.3354076	101.1544978	sumatra
way	580994606	pertamina	Pertamina	SPBU 14.293.642	-0.7668565	102.5511491	sumatra
way	582055888	pertamina	Pertamina	SPBU 24.372.30	-1.6869254	102.1534866	sumatra
way	582055889	pertamina	Pertamina	SPBU Koto Rayo	-1.8115263	102.2606356	sumatra
way	582055890	pertamina	Pertamina	SPBU Pelawan	-2.3895718	102.702858	sumatra
way	593618925	pertamina	Pertamina	SPBU Arengka	0.5049166	101.4187243	sumatra
way	593619392	pertamina	Pertamina	SPBU 14.293644 Belilas	-0.5582926	102.4113825	sumatra
way	593683244	other	\\N	\\N	0.4809537	101.4067391	sumatra
way	593780414	pertamina	Pertamina	SPBU Pertamina 13.282603	0.4545096	101.453125	sumatra
way	593866834	pertamina	Pertamina	SPBU Senamat	-1.6468601	102.1385372	sumatra
way	595216546	pertamina	Pertamina	Pertamina	3.6068657	98.4637829	sumatra
way	600849799	pertamina	Pertamina	\\N	-1.5381104	102.0988602	sumatra
way	600849817	pertamina	Pertamina	\\N	-1.9642581	102.2897355	sumatra
way	600849821	pertamina	Pertamina	SPBU Siulak	-1.9523554	101.3377049	sumatra
way	600849822	pertamina	Pertamina	SPBU Jembatan 1	-2.0680898	101.3872815	sumatra
way	600849823	pertamina	Pertamina	SPBU Kumun	-2.0826681	101.4085654	sumatra
way	600849824	pertamina	Pertamina	SPBU Sungai Liuk	-2.0394692	101.3872478	sumatra
way	600875569	pertamina	Pertamina	SPBU Simpang Babeko	-1.4922686	102.2026791	sumatra
way	600875570	pertamina	Pertamina	\\N	-1.4738065	102.1649498	sumatra
way	601133886	pertamina	Pertamina	SPBU Bangko	-2.0583413	102.2743953	sumatra
way	602039118	other	\\N	\\N	1.113956	104.0221641	sumatra
way	602562214	pertamina	Pertamina	\\N	-1.5830335	103.5563954	sumatra
way	602562217	pertamina	Pertamina	SPBU Pondok Meja	-1.6938605	103.5850762	sumatra
way	602562218	pertamina	Pertamina	\\N	-1.6692337	103.5932274	sumatra
way	602562219	pertamina	Pertamina	SPBU Paal VI	-1.6422385	103.60299	sumatra
way	602562220	pertamina	Pertamina	\\N	-1.6361317	103.6562607	sumatra
way	602562221	pertamina	Pertamina	SPBU Bandara	-1.6275102	103.6349597	sumatra
way	602562222	pertamina	Pertamina	\\N	-1.6309222	103.6247796	sumatra
way	602562223	pertamina	Pertamina	SPBU Kebun Handil	-1.6221656	103.6200106	sumatra
way	602562224	pertamina	Pertamina	\\N	-1.6127416	103.6058584	sumatra
way	602562267	pertamina	Pertamina	\\N	-1.6200073	103.5797098	sumatra
way	602562278	pertamina	Pertamina	SPBU Sungai Duren	-1.6004403	103.5021249	sumatra
way	602562283	pertamina	Pertamina	\\N	-1.6122649	103.576923	sumatra
way	602562358	pertamina	Pertamina	\\N	-1.6207574	103.5744318	sumatra
way	602562379	pertamina	Pertamina	SPBU Simpang Rimbo	-1.6236417	103.5515405	sumatra
way	602890978	pertamina	Pertamina	Pertamina	0.4084045	101.7950477	sumatra
way	605121769	pertamina	Pertamina	SPBU 24.301.11	-2.9754117	104.7830325	sumatra
way	605271177	pertamina	Pertamina	SPBU Tembesi	-1.7220649	103.1366017	sumatra
way	611344737	pertamina	Pertamina	SPBU 24.306.737	-3.2106143	104.6443732	sumatra
way	611543231	pertamina	Pertamina	\\N	-3.0057715	104.8185713	sumatra
way	611726124	pertamina	Pertamina	SPBU 14.282.6114	0.5347202	101.4224021	sumatra
way	619492029	pertamina	Pertamina	\\N	4.0085175	98.225246	sumatra
way	623363813	pertamina	Pertamina	SPBU Kampung Keramat	-2.1379633	106.0949043	sumatra
way	623363814	other	\\N	\\N	-2.1379181	106.0949405	sumatra
way	624168898	pertamina	SPBU PERTAMINA	SPBU 14.202.141	3.5677921	98.7235301	sumatra
way	624342192	pertamina	SPBU 14-239412	SPBU 14-239412	5.5560663	95.3285061	sumatra
way	624368621	other	00-44-02-A	00-44-02-A	5.5549444	95.3223667	sumatra
way	631930677	pertamina	SPBU 14201127 Sisingamangaraja	SPBU 14201127 Sisingamangaraja	3.5382044	98.7039243	sumatra
way	632817705	pertamina	Pertamina	Pertamina	-0.7735224	100.3145637	sumatra
way	634215881	pertamina	Pertamina	SPBU Dusun Bangko	-2.0753767	102.2901681	sumatra
way	634215907	pertamina	Pertamina	SPBU Mini 26.373.12	-2.1553787	102.0791712	sumatra
way	634215970	pertamina	Pertamina	SPBU Tanah Kampung	-2.0747988	101.4267691	sumatra
way	636461895	pertamina	SPBU 14,201.106	SPBU 14,201.106	3.5413156	98.6649381	sumatra
way	645708443	pertamina	Pertamina	SPBU Petai	-0.2613173	101.325097	sumatra
way	647998530	pertamina	Pertamina	SPBU Pal 3 Muntok	-2.0468504	105.1922924	sumatra
way	654417777	other	\\N	\\N	1.1540088	104.0024046	sumatra
way	668392309	pertamina	Pertamina	SPBU Persijam	-1.6120161	103.6301809	sumatra
way	679418351	pertamina	SPBU Pertamina	SPBU Pertamina	3.2006892	98.5332318	sumatra
way	680833638	pertamina	Pertamina	Pertamina	5.1702071	97.1339741	sumatra
way	686009882	pertamina	Pertamina	SPBU Liki	-1.5876645	101.1836838	sumatra
way	686055230	pertamina	Pertamina	SPBU Lubuk Selasih	-0.9658251	100.5941483	sumatra
way	686062494	pertamina	Pertamina	SPBU By Pass	-0.9128351	100.3970247	sumatra
way	686071948	pertamina	Pertamina	SPBU 14.255.577	-0.5267114	100.3376842	sumatra
way	686071956	pertamina	Pertamina	SPBU	-0.7081615	100.292212	sumatra
way	686071957	pertamina	Pertamina	SPBU 14.255.5103	-0.7267358	100.3119122	sumatra
way	686301677	pertamina	Pertamina	SPBU 14-271-528	-0.4475718	100.4005897	sumatra
way	686301678	pertamina	Pertamina	SPBU 14.271.536	-0.4687216	100.4111874	sumatra
way	686305636	pertamina	Pertamina	SPBU Sungai Pua	-0.3761287	100.3933184	sumatra
way	687341881	pertamina	Pertamina	SPBU 14.273.558	-0.7730938	100.6272642	sumatra
way	687382913	pertamina	Pertamina	SPBU 13.264.518	-0.9114572	100.6356141	sumatra
way	687382921	pertamina	Pertamina	SPBU Koto Baru	-0.829074	100.6547882	sumatra
way	693592285	pertamina	Pertamina	SPBU Merlung	-1.2706378	103.0575037	sumatra
way	707618244	pertamina	Pertamina	Pertamina	0.5629017	101.4500856	sumatra
way	733142596	pertamina	Pertamina	Pertamina	1.0370587	103.9766581	sumatra
way	733210729	pertamina	Pertamina	Pertamina	1.045599	103.9614613	sumatra
way	734005408	other	\\N	\\N	1.1391181	104.0270816	sumatra
way	734287177	other	\\N	\\N	1.1588528	104.0324118	sumatra
way	735230133	other	\\N	\\N	4.4488686	98.0121331	sumatra
way	747824298	pertamina	Pertamina	SPBU Pangkalan Kerinci	0.399926	101.8583603	sumatra
way	775888134	pertamina	Pertamina	SPBU Sengeti	-1.4707873	103.5064285	sumatra
way	775888135	pertamina	Pertamina	SPBU Bukit Baling	-1.4211139	103.4942297	sumatra
way	775888136	pertamina	Pertamina	SPBU 24.365.37	-1.3127491	103.2415791	sumatra
way	776174767	pertamina	Pertamina	SPBU Bayung Lencir	-2.0444519	103.6908497	sumatra
way	778198865	pertamina	Pertamina	SPBU 25.363.01	-1.4107	103.9440523	sumatra
way	808511290	other	\\N	\\N	4.1072634	96.2890102	sumatra
way	812393384	pertamina	Pertamina	SPBU 24.385.08	-4.451627	102.9159344	sumatra
way	819706940	pertamina	Pertamina	SPBU Bajubang	-1.7758583	103.3210905	sumatra
way	820914930	pertamina	Pertamina	SPBU Sijenjang	-1.5683207	103.648068	sumatra
way	844944919	pertamina	Pertamina	SPBU 24.372.48 Muaro Bungo	-1.4959621	102.124038	sumatra
way	856120405	pertamina	Pertamina	Pertamina	3.1357485	99.4843922	sumatra
way	869037302	other	\\N	\\N	3.4239741	98.9044399	sumatra
way	880868857	other	\\N	\\N	-2.9660866	104.7889769	sumatra
way	930576510	pertamina	Pertamina	SPBU Pamenang	-2.1054008	102.5029142	sumatra
way	930589684	pertamina	Pertamina	SPBU Bathin VIII	-2.2442638	102.5075619	sumatra
way	934148967	pertamina	Pertamina	\\N	-5.5916463	105.7894284	sumatra
way	968031297	pertamina	Pertamina	SPBU Sungai Rumbai	-1.2357016	101.7383771	sumatra
way	968031303	pertamina	Pertamina	SPBU Rantau Ikil	-1.329811	101.7677645	sumatra
way	970313193	pertamina	Pertamina	SPBU 24-363-68	-1.5457774	103.5357029	sumatra
way	971550232	pertamina	Pertamina	Pertamina	3.7064751	98.6792398	sumatra
way	971816431	other	\\N	\\N	3.6945622	98.6730771	sumatra
way	973396112	pertamina	Pertamina	SPBU Kampung Baru	-1.0833831	102.9949487	sumatra
way	974972197	pertamina	Pertamina	SPBU 24.363.59	-1.360339	103.3889221	sumatra
way	974972213	pertamina	Pertamina	SPBU Simpang Tuan	-1.2859577	103.4943731	sumatra
way	980434184	pertamina	Pertamina	SPBU 23.361.03	-1.6301302	103.6338631	sumatra
way	980558161	other	\\N	\\N	-0.2894322	100.428669	sumatra
way	984825201	pertamina	Pertamina	SPBU Margodadi	-5.387546	104.9624194	sumatra
way	995446385	other	\\N	\\N	3.5725328	98.6804609	sumatra
way	1000694853	pertamina	Pertamina	SPBU Arah Tiga	-2.4375551	101.1509599	sumatra
way	1010185373	pertamina	Pertamina	SPBU 13 273 504	-1.4289182	101.3805693	sumatra
way	1012036396	pertamina	Pertamina	SPBU 24-383-23	-2.9948042	101.4947241	sumatra
way	1014241578	other	\\N	\\N	1.1281089	104.0583837	sumatra
way	1018418201	pertamina	Pertamina	SPBU 24.363.39	-1.7971445	103.4247061	sumatra
way	1020078779	pertamina	Pertamina	SPBU Tol Bakauheni Km 116 B	-5.0906322	105.2123054	sumatra
way	1020078782	pertamina	Pertamina	SPBU Tol Bakauheni Km 116 A	-5.0892897	105.2118259	sumatra
way	1020078844	pertamina	Pertamina	Pertamina	-5.516618	105.5201818	sumatra
way	1020078895	pertamina	Pertamina	Pertamina	-5.3186889	105.2665001	sumatra
way	1020078910	pertamina	Pertamina	Pertamina	-5.3188925	105.2648035	sumatra
way	1020270812	pertamina	Pertamina	Pertamina	-4.2505361	105.1382514	sumatra
way	1020270873	pertamina	Pertamina	Pertamina	-4.6056099	105.1818991	sumatra
way	1020270900	pertamina	Pertamina	Pertamina	-4.680718	105.1919812	sumatra
way	1023510969	pertamina	SPBU Sinabang	SPBU Sinabang	2.4685363	96.3822708	sumatra
way	1024621245	pertamina	Pertamina	SPBU Pertamina [Itera - Kota Baru]	-5.3470985	105.3324272	sumatra
way	1025143778	pertamina	Pertamina	SPBU Sarolangun	-2.3119176	102.7317644	sumatra
way	1027385145	pertamina	SPBU COCO Putri Hijau Medan	SPBU COCO Putri Hijau Medan	3.6023869	98.6736243	sumatra
way	1031084458	pertamina	Pertamina	SPBU Muara Aman	-3.123911	102.2075896	sumatra
way	1036471173	pertamina	SPBU Pertamina	SPBU Pertamina	3.5401361	98.6541388	sumatra
way	1036515496	other	\\N	\\N	3.6086659	98.6085183	sumatra
way	1040556925	pertamina	Pertamina	Pertamina	3.6728303	98.6820627	sumatra
way	1040556926	pertamina	Pertamina	Pertamina	3.6731071	98.6758057	sumatra
way	1040558131	pertamina	Pertamina	Pertamina	3.6925919	98.6793344	sumatra
way	1040828507	shell	Shell	Shell	3.6065153	98.6719057	sumatra
way	1041076885	pertamina	Pertamina	SPBU Bengkolan Dua	-1.7605867	101.3067587	sumatra
way	1041191545	pertamina	Pertamina	Pertamina	-0.3993733	102.4092551	sumatra
way	1041346885	pertamina	Pertamina	Pertamina	3.6790384	98.6556765	sumatra
way	1041989471	pertamina	Pertamina	Singapore Station	3.7816764	98.6878276	sumatra
way	1041989475	pertamina	Pertamina	Pertamina	3.7718092	98.6858335	sumatra
way	1042166092	pertamina	Pertamina	SPBU Coco 21.301.01	-2.9588755	104.7701726	sumatra
way	1042166101	pertamina	Pertamina	SPBG R. Soekamto	-2.9518748	104.7590354	sumatra
way	1042168546	pertamina	Pertamina	SPBU Alahan Panjang	-1.0587892	100.7570994	sumatra
way	1042506922	pertamina	Pertamina	SPBU Pasar Bukit Air Haji	-1.9414833	100.9011794	sumatra
way	1042869615	pertamina	Pertamina	SPBU Pembengis	-0.8466475	103.4241058	sumatra
way	1042977178	pertamina	Pertamina	Pertamina	3.6115517	98.6658464	sumatra
way	1042979503	pertamina	Pertamina	Pertamina 14.202.1141	3.7013366	98.658411	sumatra
way	1043391217	pertamina	Pertamina	SPBU Sabak	-1.215375	103.794301	sumatra
way	1044415558	pertamina	Pertamina	Pertamina	3.2904182	98.5657652	sumatra
way	1044419114	pertamina	Pertamina	Pertamina	3.5249383	99.0559858	sumatra
way	1044419115	pertamina	Pertamina	Pertamina	3.5251319	99.0576592	sumatra
way	1044796413	pertamina	Pertamina	Pertamina	3.353614	99.167075	sumatra
way	1044799487	pertamina	Pertamina	Pertamina	3.3472429	99.1680082	sumatra
way	1046036813	pertamina	Pertamina	SPBU Pertamina Kauman Metro	-5.1178362	105.3077894	sumatra
way	1046406468	pertamina	Pertamina	Pertamina	1.7318171	100.3566943	sumatra
way	1046425955	pertamina	Pertamina	Pertamina	1.6282573	101.0060267	sumatra
way	1046466066	pertamina	Pertamina	\\N	-1.5991934	103.6189159	sumatra
way	1046518124	pertamina	Pertamina	Pertamina	0.3906591	101.8248571	sumatra
way	1049508907	pertamina	Pertamina	Pertamina	3.5989133	98.6446149	sumatra
way	1050447553	pertamina	Pertamina	pertamina	2.575317	99.6532056	sumatra
way	1050452511	pertamina	Pertamina	Pertamina	3.1224013	99.5563468	sumatra
way	1051571756	pertamina	Pertamina	Pertamina	-0.13735	102.1631328	sumatra
way	1052201854	pertamina	Pertamina	SPBU Karang Mendapo	-2.2118364	102.7850925	sumatra
way	1052461847	other	\\N	\\N	1.0679925	104.2288677	sumatra
way	1054988229	pertamina	Pertamina	Pertamina	3.639967	98.7170138	sumatra
way	1055261637	pertamina	Pertamina	SPBU Pertamina 14.294.742	1.1322239	104.0112631	sumatra
way	1055349152	pertamina	Pertamina	SPPBE Tebing Tinggi	-1.6423009	103.3334779	sumatra
way	1058978472	pertamina	Pertamina	SPBU Tabing	-0.8886996	100.3515967	sumatra
way	1058978473	pertamina	Pertamina	SPBU 11.251.502	-0.9054644	100.35058	sumatra
way	1063425580	pertamina	Pertamina	SPBU PT Cahya Hartawan	-5.1320335	105.3204401	sumatra
way	1063450808	pertamina	SPBU 14.201.1168	SPBU 14.201.1168	3.6151038	98.6436626	sumatra
way	1063450809	pertamina	SPBU 14.203.133	SPBU 14.203.133	3.6158428	98.644936	sumatra
way	1064228799	pertamina	Pertamina	SPBU 14 208 1160	3.7698763	98.442851	sumatra
way	1064233134	pertamina	SPBU Pertamina	SPBU Pertamina	3.5604577	98.8800202	sumatra
way	1064246151	other	POM Bensin Sukaraja	POM Bensin Sukaraja	-5.4451907	105.2939013	sumatra
way	1064250543	pertamina	Pertamina	SPBU Branti	-5.2308348	105.1736298	sumatra
way	1064260792	pertamina	SPBU Pertamina 14 205 164	SPBU Pertamina 14 205 164	3.5537657	98.8735219	sumatra
way	1064279380	pertamina	SPBU Pertamina 14 2051105	SPBU Pertamina 14 2051105	3.5627158	98.8999034	sumatra
way	1064987080	pertamina	Pertamina	SPBU Ganjaran	-5.3689359	104.9245227	sumatra
way	1065025404	pertamina	SPBU Shel SM Raja dua	SPBU Shel SM Raja dua	3.5684252	98.6910029	sumatra
way	1067451168	pertamina	SPBU Pertamina	SPBU Pertamina	3.5250413	98.7928055	sumatra
way	1067451169	pertamina	SPBU Pertamina	SPBU Pertamina	3.5265578	98.7952627	sumatra
way	1068910038	pertamina	Pertamina	SPBU Simpang Jam	5.5473201	95.316114	sumatra
way	1081571495	pertamina	Pertamina	SPBU 24.301.07	-2.9725373	104.730168	sumatra
way	1128428891	other	SPBI BANGKO	SPBI BANGKO	-3.765645	103.8310317	sumatra
way	1132088374	other	SPBI	SPBI	-3.7186414	103.7877152	sumatra
way	1133631070	pertamina	Pertamina	SPBU 13.275.512	-1.1582543	101.7190747	sumatra
way	1137161545	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8722656	100.3824689	sumatra
way	1137161546	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8723079	100.3824149	sumatra
way	1137161547	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8723556	100.3823513	sumatra
way	1137161548	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8724043	100.3822946	sumatra
way	1137161549	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8724421	100.3822391	sumatra
way	1137161550	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8722257	100.3825237	sumatra
way	1137161551	pertamina	Pertamina	Pertamina SPBU Aie Pacah By Pass Padang	-0.8721804	100.382582	sumatra
way	1152634789	pertamina	Pertamina	Pertamina	2.9590481	99.0452509	sumatra
way	1152634790	pertamina	Pertamina	Pertamina	2.9850953	99.1982811	sumatra
way	1152634791	pertamina	Pertamina	Pertamina	3.1694306	99.367729	sumatra
way	1158638759	pertamina	Pertamina	SPBU 13.262.515 Koto Baru	-0.2648497	100.5665245	sumatra
way	1184944630	pertamina	Pertamina	Pertamina	-4.3581444	105.2320479	sumatra
way	1184944633	pertamina	Pertamina	SPBU Unit 2 Tulang Bawang	-4.2939646	105.2211143	sumatra
way	1185339416	pertamina	Pertamina	Pertamina	-4.0804193	105.1591248	sumatra
way	1185624451	pertamina	Pertamina	Pertamina	-4.2899429	105.2746305	sumatra
way	1185626857	pertamina	Pertamina	Pertamina	-4.2661244	105.2408203	sumatra
way	1185626858	other	\\N	\\N	-4.2661289	105.2408143	sumatra
way	1185871329	pertamina	Pertamina	Pertamina	-4.5003524	105.243512	sumatra
way	1185871972	pertamina	Pertamina	Pertamina	-4.1883211	105.225268	sumatra
way	1186355567	pertamina	Pertamina	Pertamina	-4.2143633	105.7275345	sumatra
way	1186357627	pertamina	Pertamina	Pertamina	-4.1793591	105.4082155	sumatra
way	1188277899	pertamina	Pertamina	Pertamina	-4.4401365	105.2687124	sumatra
way	1188277900	pertamina	Pertamina	Pertamina	-4.471956	105.2648472	sumatra
way	1190543457	pertamina	SPBU MATANGKULI	SPBU MATANGKULI	5.0359998	97.276939	sumatra
way	1190777198	pertamina	SPBU Lhoksukon	SPBU Lhoksukon	5.0425888	97.3276245	sumatra
way	1190790718	pertamina	SPBU Ranto	SPBU Ranto	5.0614199	97.3118597	sumatra
way	1190838708	pertamina	SPBU MINI	SPBU MINI	5.1040791	97.3185857	sumatra
way	1190839561	pertamina	SPBU Senggarang	SPBU Senggarang	0.9587972	104.4518195	sumatra
way	1191357706	pertamina	SPBU Simpang Rangkaya	SPBU Simpang Rangkaya	5.0581892	97.2600183	sumatra
way	1196784577	other	\\N	\\N	-4.9831107	105.3167768	sumatra
way	1201939801	pertamina	Pertamina	SPBU Bagan Pete	-1.6508904	103.5609784	sumatra
way	1203870120	pertamina	Pertamina	SPBU 24.361.35	-1.6036432	103.6018771	sumatra
way	1204076322	pertamina	Pertamina	SPBU 24.361.77	-1.644269	103.5805743	sumatra
way	1204106669	pertamina	Pertamina	SPBU Paal V	-1.6223638	103.6026738	sumatra
way	1205205984	pertamina	Pertamina	SPBU Muaradua	-4.5442926	104.0731422	sumatra
way	1205735203	pertamina	Pertamina	SPBU 24.361.09	-1.6036563	103.6406031	sumatra
way	1208691619	pertamina	Pertamina	SPBU Tembilahan Hulu	-0.3466001	103.1211545	sumatra
way	1208698786	pertamina	Pertamina	SPBU 24.33.1127	-2.1317681	106.0567825	sumatra
way	1208698789	pertamina	Pertamina	SPBU 24.331.141	-2.0233803	105.9410885	sumatra
way	1208698792	pertamina	Pertamina	SPBU 24.333.129	-2.0413189	105.2208996	sumatra
way	1208698799	pertamina	Pertamina	SPBU 24.333.97	-1.8738528	105.4661236	sumatra
way	1208698804	pertamina	Pertamina	SPBU 24.333.125	-1.871265	105.6588806	sumatra
way	1208698805	pertamina	Pertamina	SPPBE	-1.871167	105.662346	sumatra
way	1208711896	pertamina	Pertamina	SPBU Singkut	-2.5133003	102.7131441	sumatra
way	1208711900	pertamina	Pertamina	SPBU 25.372.09	-1.6113707	102.3409916	sumatra
way	1208711904	pertamina	Pertamina	SPBU 23.372.05	-1.3749911	101.8920067	sumatra
way	1208711906	pertamina	Pertamina	SPBU Simpang Lubuk Landai	-1.3829357	101.9331399	sumatra
way	1208711910	pertamina	Pertamina	SPBU 25.363.07	-2.006056	103.4096308	sumatra
way	1208980462	pertamina	Pertamina	SPBU Kepahiang	-3.6450037	102.5801453	sumatra
way	1208980467	pertamina	Pertamina	SPBU 21.391.11	-3.6268304	102.5579074	sumatra
way	1208980474	pertamina	Pertamina	SPBU Pekalongan	-3.5268559	102.5190715	sumatra
way	1208980477	pertamina	Pertamina	SPBU Simpang Nangka	-3.4508872	102.5816941	sumatra
way	1208980482	pertamina	Pertamina	SPBU Padang Ulak Tanding	-3.3634024	102.8085626	sumatra
way	1208980485	pertamina	Pertamina	SPBU 24.386.10	-3.4365722	102.21555	sumatra
way	1208980487	pertamina	Pertamina	SPBU 24.386.28	-3.4449363	102.1620472	sumatra
way	1208980493	pertamina	Pertamina	SPBU 24.383.31	-2.6536775	101.3162568	sumatra
way	1208980494	pertamina	Pertamina	SPBU 24.383.17	-2.640497	101.1615189	sumatra
way	1212715728	pertamina	Pertamina	SPBU Aur Ringit	-4.6054299	103.1765453	sumatra
way	1214951873	pertamina	Pertamina	SPBU 24.385.09	-4.4791182	102.9292386	sumatra
way	1215027388	pertamina	Pertamina	SPBU Ahmad Yani	-4.4564571	102.9216249	sumatra
way	1215203889	pertamina	Pertamina	Pertamina	-5.3022579	105.762939	sumatra
way	1216157034	pertamina	Pertamina	Pertamina KDA Batam Kota	1.1015303	104.07652	sumatra
way	1218745768	pertamina	Pertamina	SPBU Keritang	-0.8791918	102.6588519	sumatra
way	1218745774	pertamina	Pertamina	SPBU Selensen	-0.9710549	102.770856	sumatra
way	1221939550	pertamina	Pertamina	SPBU Talo	-4.1661129	102.7022336	sumatra
way	1231274906	pertamina	Pertamina	\\N	-3.2789561	102.864307	sumatra
way	1253091009	pertamina	Pertamina	SPBU NAPCO	0.5384763	101.4387297	sumatra
way	1254516112	pertamina	Pertamina	SPBU Pematang Reba	-0.4061753	102.4502001	sumatra
way	1255870079	pertamina	Pertamina	SPBU 14.293.640	-0.5294848	101.5557636	sumatra
way	1256915830	pertamina	Pertamina	SPBU Sei Kijang	0.4230483	101.6473684	sumatra
way	1261880102	pertamina	Pertamina	SPBU Pasar Atas Bangko	-2.0913409	102.2528999	sumatra
way	1261901609	pertamina	Pertamina	SPBU 24.373.80	-2.0716104	102.4191725	sumatra
way	1262006935	pertamina	Pertashop	\\N	-1.9848951	102.9902718	sumatra
way	1268686219	pertamina	Pertamina	SPBU 24.383.22	-2.5550575	101.1016364	sumatra
way	1268695256	pertamina	Pertamina	SPBU 24.365.52	-1.058332	103.3830195	sumatra
way	1268697234	pertamina	Pertamina	SPBU 14.275.5104	-0.9147558	101.427039	sumatra
way	1268699586	pertamina	Pertamina	SPBU Rimbo Bujang	-1.3173693	102.1015875	sumatra
way	1268702331	pertamina	Pertamina	SPBU 24.345.81	-4.0216034	105.221447	sumatra
way	1269057127	pertamina	Pertamina	Agen Premium Minyak dan Solar 26.01.28	-3.5654919	105.0600698	sumatra
way	1269058822	pertamina	Pertamina	SPBU 24.306.148	-3.7868544	104.9081495	sumatra
way	1269077898	pertamina	Pertamina	SPBU 24.306.139	-3.7265915	104.8174714	sumatra
way	1270386840	pertamina	Pertamina	SPBU 24.306.106	-3.3010401	104.6939507	sumatra
way	1270657143	pertamina	Pertamina	\\N	-2.9242574	104.6814046	sumatra
way	1270824332	pertamina	Pertamina	SPBU 24.301.15	-2.9194638	104.7064028	sumatra
way	1270848457	pertamina	Pertamina	SPBU 24.301.176	-2.9869885	104.749967	sumatra
way	1270911604	pertamina	Pertamina	SPBU 24.316.91	-3.1807079	102.9477592	sumatra
way	1271646245	pertamina	Pertamina	SPBU Kemelak Baturaja	-4.1525601	104.1818372	sumatra
way	1271650368	pertamina	Pertamina	SPBU Batumarta	-4.2142746	104.2508577	sumatra
way	1271653215	pertamina	Pertamina	SPBU Kota Baru Martapura	-4.3319619	104.3506869	sumatra
way	1271705229	pertamina	Pertamina	SPBU UB Baturaja	-4.1396415	104.1819036	sumatra
way	1271709154	pertamina	Pertamina	SPBU Air Karang	-4.1307955	104.1777439	sumatra
way	1271751943	pertamina	Pertamina	SPBU Inderapura	-2.0434329	100.9918402	sumatra
way	1271751944	pertamina	Pertamina	SPBU 14.256.569	-1.6428226	100.6725164	sumatra
way	1271751945	pertamina	Pertamina	SPBU Tapan	-2.1353035	101.0794495	sumatra
way	1271751955	pertamina	Pertamina	SPBU 14.256.107	-1.7821208	100.8113217	sumatra
way	1271751956	pertamina	Pertamina	SPBU Batang Kapas	-1.4472417	100.5996842	sumatra
way	1271751958	pertamina	Pertamina	SPBU Simpang Lagan	-1.8696127	100.8536989	sumatra
way	1271751962	pertamina	Pertamina	SPBU Taratak	-1.5532063	100.63801	sumatra
way	1274415428	pertamina	Pertamina	SPBU 24.373.84	-2.0606205	102.3206469	sumatra
way	1287004473	pertamina	Pertamina	SPBU 24.316.53	-2.7391317	102.8979259	sumatra
way	1294787438	pertamina	Pertamina	SPBU Pasar Pagi Soekarno Hatta	0.4668026	101.4189411	sumatra
way	1296575720	other	Pom Bensin	Pom Bensin	-5.1337363	105.4095869	sumatra
way	1299195226	pertamina	Pertamina	SPBU Pertamina 13.282604	0.4838467	101.5047558	sumatra
way	1307652744	pertamina	Pertamina	Pertamina	0.5284724	101.4539348	sumatra
way	1311181887	pertamina	Pertamina	\\N	1.4070554	99.4392653	sumatra
way	1317427738	pertamina	Pertamina	Pertamina 13.282.621	0.4660712	101.4938936	sumatra
way	1324274951	pertamina	Pertashop Sukajadi	\\N	-5.3768753	104.8625189	sumatra
way	1326431615	pertamina	Pertamina	Pertamina	-5.2924097	105.7552304	sumatra
way	1328835783	pertamina	SPBU SRIMENANTI	SPBU SRIMENANTI	-5.3030748	105.7445378	sumatra
way	1329256174	pertamina	SPBU Pertamina 24.341.71	SPBU Pertamina 24.341.71	-5.1320743	105.6932977	sumatra
way	1349363892	pertamina	Pertamina	Pertamina	5.3167706	95.5938135	sumatra
way	1350536125	pertamina	Pertamina	Pertamina	4.1535923	96.1435897	sumatra
way	1350536142	pertamina	Pertamina	Pertamina	4.1867993	96.0973013	sumatra
way	1350747549	pertamina	Pertamina	Pertamina	4.635148	95.5996299	sumatra
way	1350747556	pertamina	Pertamina	Pertamina	4.6325945	95.5888992	sumatra
way	1351218425	pertamina	Pertamina	Pertamina	3.1788201	97.2896178	sumatra
way	1351845362	other	\\N	\\N	-5.4932431	105.3263263	sumatra
way	1352481893	pertamina	PERTAMINA	SPBUSimpang Semambang	-3.1371291	103.1295304	sumatra
way	1355623336	pertamina	Pertamina	\\N	-2.5949713	102.7493286	sumatra
way	1363306706	pertamina	Pertamina	SPBU Tebing Tinggi	-1.6318107	103.3400069	sumatra
way	1377979908	pertamina	Pertamina	SPBU Jake	-0.5111428	101.5069106	sumatra
way	1379873486	pertamina	Pertamina	\\N	-2.9967149	102.7941991	sumatra
way	1380186611	pertamina	Pertamina	\\N	-3.2733572	102.9274215	sumatra
way	1381874558	pertamina	Pertamina	SPBU Babat Toman	-2.7396271	103.6322406	sumatra
way	1382704875	pertamina	Pertamina	SPBU Merdeka	-2.8960982	103.8492069	sumatra
way	1387719442	pertamina	Pertamina	SPBU Kota Bani	-3.2528782	101.6568998	sumatra
way	1388694418	pertamina	SPBU MADAT ACEH TIMUR	SPBU MADAT ACEH TIMUR	5.1140106	97.46279	sumatra
way	1389221353	pertamina	Pertashop CV Marjansa Utama	Pertashop CV Marjansa Utama	5.1800784	97.3702356	sumatra
way	1389982267	pertamina	Pertashop	\\N	-3.2964571	102.4214139	sumatra
way	1390124027	pertamina	Pertamina	Pertamina 16.287.616	0.8348737	101.6753658	sumatra
way	1390626978	pertamina	PERTASHOP	PERTASHOP	-3.5077156	102.6959049	sumatra
way	1390660834	pertamina	Pertamina	SPBU Perikanan	-1.6067566	103.5408314	sumatra
way	1391470986	pertamina	SPBU	SPBU	5.1101891	97.3570147	sumatra
way	1391507705	pertamina	Pertamina	Pertamina	-4.4022087	105.2589114	sumatra
way	1391749684	pertamina	Pertamina	SPBU Logas	-0.4318274	101.3770782	sumatra
way	1392013182	pertamina	Pertamina	SPBU Pesikaian	-0.5008208	101.8975844	sumatra
way	1409388895	pertamina	Pertamina	Pertamina 14.282.627	0.4621391	101.5742827	sumatra
way	1409392378	pertamina	Pertamina	SPBU 24.345.23	-4.5840435	104.5001165	sumatra
way	1409419100	pertamina	Pertamina	SPBU Gunung Katun	-4.6642104	104.5411931	sumatra
way	1409419106	pertamina	Pertamina	SPBU Bumi Ratu	-4.6041065	104.541102	sumatra
way	1410853869	pertamina	Pertamina	SPBU 24.307.158	-2.8676929	103.8432342	sumatra
way	1411126413	pertamina	Pertamina	SPBU Tanjung Kerang	-2.7894982	104.1111411	sumatra
way	1411126416	pertamina	Pertamina	SPBU Sukamaju	-2.6496756	104.0867751	sumatra
way	1412105051	pertamina	Pertamina	SPBU 24.316.187	-3.2578338	102.9902162	sumatra
way	1418836586	pertamina	Pertamina	SPBU Talang Gulo	-1.6750069	103.6125655	sumatra
way	1418904513	pertamina	Pertamina	SPBU Pertamina 11.282604	0.5147371	101.4332305	sumatra
way	1418904515	pertamina	Pertamina	SPBU Pertamina 13.282605	0.5242551	101.4363812	sumatra
way	1418904517	pertamina	Pertamina	SPBU Pertamina 13.282606	0.4820695	101.4683443	sumatra
way	1418904520	pertamina	Pertamina	SPBU Pertamina 13.282610	0.4907435	101.4391811	sumatra
way	1419525834	pertamina	Pertamina	Pertamina Batu Besurat	0.3082842	100.7625273	sumatra
way	1419627946	pertamina	Pertamina	Pertamina	-3.2225843	104.4758056	sumatra
way	1419934365	pertamina	Pertamina	SPBU 24-361-90	-3.2944983	103.0594814	sumatra
way	1420318612	pertamina	Pertamina	SPBU Tebing Tinggi	-3.5656412	103.0760968	sumatra
way	1420902619	pertamina	Pertamina	SPBU Batu Gajah	-0.3650339	102.2611875	sumatra
way	1455381679	other	\\N	\\N	4.1816584	98.0521899	sumatra
way	1458622541	pertamina	Pertamina	SPBU Sungai Rengas	-1.6253645	102.8655855	sumatra
way	1461445023	pertamina	Pertamina	Pertamina	3.8668877	98.4238251	sumatra
way	1462980927	pertamina	Pertamina	Pertamina	0.4165233	101.2788432	sumatra
way	1462980929	pertamina	Pertamina	Pertamina	0.3997458	101.2537612	sumatra
way	1464063882	pertamina	Pertamina	Pertashop	0.8419626	104.6046621	sumatra
way	1464064330	pertamina	Pertamina	SPBU 13.291.701 KM 19 Kijang	0.8897621	104.5737222	sumatra
way	1464109550	pertamina	Pertamina	Pertamina	0.9200676	104.5115094	sumatra
way	1464226071	pertamina	Pertamina	Pertamina	3.4626831	97.8272923	sumatra
way	1466924633	pertamina	Pertamina	Pertamina	1.4206809	99.2892182	sumatra
way	1469031851	pertamina	Pertamina	\\N	-1.6993648	103.4594991	sumatra
way	1469929873	other	\\N	\\N	4.0175906	97.2952708	sumatra
way	1470182378	pertamina	Pertamina	SPBU 24.302.19	-3.0381557	104.7511517	sumatra
way	1470183456	pertamina	Pertamina	SPBU 24.302.23	-3.0798083	104.7265204	sumatra
way	1470184859	pertamina	Pertamina	SPBU 24.306.26	-3.2429735	104.6691314	sumatra
way	1470191284	pertamina	Pertamina	SPBU 23.306.23	-3.0524836	104.7662834	sumatra
way	1470228199	pertamina	Pertamina	SPBU 24.302.175	-3.0033217	104.7719742	sumatra
way	1473292375	pertamina	Pertamina	Pertamina	1.5551415	101.3799624	sumatra
way	1478821147	pertamina	Pertamina	Pertashop	-3.0220811	104.7510094	sumatra
way	1481714829	pertamina	Pertamina	SPBU 24.302.126	-3.0017202	104.7652847	sumatra
way	1515967852	pertamina	Pertamina	SPBU 16 228 521	1.3942334	97.4530888	sumatra
way	1533434840	pertamina	Pertamina	Pertashop	-3.1117053	104.7610482	sumatra
\.

INSERT INTO fuel_station (osm_type, osm_id, brand, brand_raw, name, lat, lon, island)
SELECT osm_type, osm_id, brand, brand_raw, name, lat, lon, island FROM fuel_station_load
ON CONFLICT (osm_type, osm_id) DO UPDATE SET
  brand      = EXCLUDED.brand,
  brand_raw  = COALESCE(EXCLUDED.brand_raw, fuel_station.brand_raw),
  name       = COALESCE(EXCLUDED.name, fuel_station.name),
  lat        = EXCLUDED.lat,
  lon        = EXCLUDED.lon,
  island     = EXCLUDED.island,
  updated_at = now();

COMMIT;

SELECT brand, count(*) FROM fuel_station GROUP BY brand ORDER BY 2 DESC;
